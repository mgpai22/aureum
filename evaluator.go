package aureum

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"

	apolloUTxO "github.com/Salvionied/apollo/serialization/UTxO"
	apolloCbor "github.com/Salvionied/cbor/v2"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

type Evaluator struct {
	runtime           wazero.Runtime
	module            api.Module
	evalPhaseTwoRaw   api.Function
	alloc             api.Function
	dealloc           api.Function
	utxoToInputBytes  api.Function
	utxoToOutputBytes api.Function
	config            EvaluatorConfig
}

func NewEvaluator(ctx context.Context, config EvaluatorConfig) (*Evaluator, error) {
	runtime := wazero.NewRuntime(ctx)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, runtime); err != nil {
		runtime.Close(ctx)
		return nil, err
	}

	var wasmBytes []byte
	var err error

	if config.WasmFile != nil {
		// Use custom WASM file if provided
		wasmBytes, err = os.ReadFile(*config.WasmFile)
		if err != nil {
			runtime.Close(ctx)
			return nil, fmt.Errorf("failed to read custom WASM file: %w", err)
		}
	} else {
		// Use embedded default WASM
		wasmBytes = defaultWasmBytes
	}

	modConfig := wazero.NewModuleConfig().
		WithStdout(os.Stdout).
		WithStderr(os.Stderr)

	module, err := runtime.InstantiateWithConfig(ctx, wasmBytes, modConfig)
	if err != nil {
		runtime.Close(ctx)
		return nil, err
	}

	evaluator := &Evaluator{
		runtime:           runtime,
		module:            module,
		evalPhaseTwoRaw:   module.ExportedFunction("eval_phase_two_raw"),
		alloc:             module.ExportedFunction("alloc"),
		dealloc:           module.ExportedFunction("dealloc"),
		utxoToInputBytes:  module.ExportedFunction("utxo_to_input_bytes"),
		utxoToOutputBytes: module.ExportedFunction("utxo_to_output_bytes"),
		config:            config,
	}

	return evaluator, nil
}

// Close terminates the WASM runtime and releases resources.
func (e *Evaluator) Close(ctx context.Context) {
	e.module.Close(ctx)
	e.runtime.Close(ctx)
}

// Evaluate processes the transaction bytes and returns redeemers as bytes.
func (e *Evaluator) Evaluate(ctx context.Context, txBytes []byte, utxos []apolloUTxO.UTxO) ([][]byte, error) {
	tx, err := GetTxFromBytes(txBytes)
	if err != nil {
		return nil, err
	}

	utxoMap := make(map[string]apolloUTxO.UTxO)
	for _, utxo := range utxos {
		key := fmt.Sprintf("%s:%d",
			hex.EncodeToString(utxo.Input.TransactionId),
			utxo.Input.Index)
		utxoMap[key] = utxo
	}

	var inputBytes [][]byte
	var outputBytes [][]byte

	// Process each input in the transaction
	for _, input := range tx.TransactionBody.Inputs {
		key := fmt.Sprintf("%s:%d",
			hex.EncodeToString(input.TransactionId),
			input.Index)

		apolloUTxO, exists := utxoMap[key]
		if !exists {
			return nil, fmt.Errorf("missing UTxO for input: %s", key)
		}

		assetMap := prepareAssetMap(&apolloUTxO)
		utxo, err := prepareUTxO(&apolloUTxO, assetMap)
		if err != nil {
			return nil, err
		}

		utxoCbor, err := apolloCbor.Marshal(utxo)
		if err != nil {
			return nil, err
		}

		utxoPtr, utxoLen := e.writeToMemory(ctx, utxoCbor)
		defer e.deallocMemory(ctx, utxoPtr, utxoLen)

		inputUtxoBytes, err := e.callFunction(ctx, e.utxoToInputBytes, utxoPtr, utxoLen)
		if err != nil {
			return nil, err
		}

		inputBytes = append(inputBytes, inputUtxoBytes)

		outputUtxoBytes, err := e.callFunction(ctx, e.utxoToOutputBytes, utxoPtr, utxoLen)
		if err != nil {
			return nil, err
		}
		outputUtxoBytesCopy := make([]byte, len(outputUtxoBytes))
		copy(outputUtxoBytesCopy, outputUtxoBytes)
		outputBytes = append(outputBytes, outputUtxoBytesCopy)
	}

	serializedUtxos := serializeUTxOs(inputBytes, outputBytes)

	txPtr, txLen := e.writeToMemory(ctx, txBytes)
	utxosPtr, utxosLen := e.writeToMemory(ctx, serializedUtxos)
	costModelsPtr, costModelsLen := e.writeToMemory(ctx, e.config.CostModels)

	defer e.deallocMemory(ctx, txPtr, txLen)
	defer e.deallocMemory(ctx, utxosPtr, utxosLen)
	defer e.deallocMemory(ctx, costModelsPtr, costModelsLen)

	results, err := e.evalPhaseTwoRaw.Call(ctx,
		txPtr, txLen,
		utxosPtr, utxosLen,
		costModelsPtr, costModelsLen,
		e.config.MaxTxExSteps, e.config.MaxTxExMem,
		e.config.ZeroTime, e.config.ZeroSlot, e.config.SlotLength,
	)
	if err != nil {
		return nil, err
	}

	resultPtr := uint32(results[0] >> 32)
	resultLen := uint32(results[0])

	resultBytes, ok := e.module.Memory().Read(resultPtr, resultLen)
	if !ok {
		return nil, errors.New("failed to read result memory")
	}

	defer e.deallocMemory(ctx, uint64(resultPtr), uint64(resultLen))

	if len(resultBytes) == 0 {
		return nil, errors.New("empty result from WASM evaluation")
	}

	if resultBytes[0] == 0 {
		var cborArray [][]byte
		decoder := apolloCbor.NewDecoder(bytes.NewReader(resultBytes[1:]))
		err := decoder.Decode(&cborArray)
		if err != nil {
			return nil, err
		}
		return cborArray, nil
	}

	var evalError EvalError
	err = apolloCbor.Unmarshal(resultBytes[1:], &evalError)
	if err != nil {
		return nil, err
	}

	return nil, &EvaluationError{EvalError: evalError}
}

// writeToMemory allocates memory in WASM and writes data to it.
func (e *Evaluator) writeToMemory(ctx context.Context, data []byte) (uint64, uint64) {
	results, err := e.alloc.Call(ctx, uint64(len(data)))
	if err != nil {
		log.Fatalf("Failed to allocate memory: %v", err)
	}
	ptr := results[0]
	if !e.module.Memory().Write(uint32(ptr), data) {
		log.Fatalf("Failed to write data to WASM memory")
	}
	return ptr, uint64(len(data))
}

// deallocMemory deallocates memory in WASM.
func (e *Evaluator) deallocMemory(ctx context.Context, ptr, size uint64) {
	_, err := e.dealloc.Call(ctx, ptr, size)
	if err != nil {
		log.Printf("Failed to deallocate memory: %v", err)
	}
}

// callFunction invokes a WASM function and retrieves the result bytes.
func (e *Evaluator) callFunction(ctx context.Context, fn api.Function, args ...uint64) ([]byte, error) {
	results, err := fn.Call(ctx, args...)
	if err != nil {
		return nil, err
	}
	if len(results) < 1 {
		return nil, errors.New("no results from function call")
	}

	resultPtr := uint32(results[0] >> 32)
	resultLen := uint32(results[0])

	resultBytes, ok := e.module.Memory().Read(resultPtr, resultLen)
	if !ok {
		return nil, errors.New("failed to read function result memory")
	}

	// Create a copy of the bytes before deallocation
	resultCopy := make([]byte, len(resultBytes))
	copy(resultCopy, resultBytes)

	e.deallocMemory(ctx, uint64(resultPtr), uint64(resultLen))

	return resultCopy, nil
}
