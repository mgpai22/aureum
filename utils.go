package aureum

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/Salvionied/apollo/serialization"
	"github.com/Salvionied/apollo/serialization/Address"
	"github.com/Salvionied/apollo/serialization/Amount"
	"github.com/Salvionied/apollo/serialization/Asset"
	"github.com/Salvionied/apollo/serialization/AssetName"
	"github.com/Salvionied/apollo/serialization/MultiAsset"
	"github.com/Salvionied/apollo/serialization/PlutusData"
	"github.com/Salvionied/apollo/serialization/Policy"
	"github.com/Salvionied/apollo/serialization/Transaction"
	"github.com/Salvionied/apollo/serialization/TransactionInput"
	"github.com/Salvionied/apollo/serialization/TransactionOutput"
	apolloUTxO "github.com/Salvionied/apollo/serialization/UTxO"
	"github.com/Salvionied/apollo/serialization/Value"
	base "github.com/Salvionied/apollo/txBuilding/Backend/Base"
	apolloCbor "github.com/Salvionied/cbor/v2"
)

// prepareAssetMap constructs a map of asset IDs to their amounts.
func prepareAssetMap(bfUtxo *apolloUTxO.UTxO) map[string]uint64 {
	assets := bfUtxo.Output.GetAmount().GetAssets()
	assetMap := make(map[string]uint64)
	assetMap["lovelace"] = uint64(bfUtxo.Output.GetAmount().GetCoin())

	for policyId, assetGroup := range assets {
		for assetName, amount := range assetGroup {
			assetId := policyId.Value + assetName.HexString()
			assetMap[assetId] = uint64(amount)
		}
	}

	return assetMap
}

// prepareUTxO constructs a UTxO struct from the UTxO data.
func prepareUTxO(apolloUTxO *apolloUTxO.UTxO, assetMap map[string]uint64) (UTxO, error) {
	var datumHashStr *string
	if datumHash := apolloUTxO.Output.GetDatumHash(); datumHash != nil {
		str := hex.EncodeToString(datumHash.Payload)
		if str != "" {
			datumHashStr = &str
		}
	}

	var datumStr *string
	if datum := apolloUTxO.Output.GetDatum(); datum != nil {
		datumCbor, err := datum.MarshalCBOR()
		if err == nil && len(datumCbor) > 0 {
			str := hex.EncodeToString(datumCbor)
			if str != "" {
				datumStr = &str
			}
		}
	}

	var scriptRef *ScriptRef
	if ref := apolloUTxO.Output.GetScriptRef(); ref != nil && ref.Script.Script != nil {
		scriptBytes := ref.Script.Script
		if len(scriptBytes) > 0 {
			scriptRef = &ScriptRef{
				ScriptType: "plutus_v2",
				Script:     hex.EncodeToString(scriptBytes),
			}
		}
	}

	utxo := UTxO{
		Address:     apolloUTxO.Output.GetAddress().String(),
		TxHash:      hex.EncodeToString(apolloUTxO.Input.TransactionId),
		OutputIndex: uint64(apolloUTxO.Input.Index),
		Assets:      assetMap,
	}

	if datumHashStr != nil {
		utxo.DatumHash = datumHashStr
	}
	if datumStr != nil {
		utxo.Datum = datumStr
	}
	if scriptRef != nil {
		utxo.ScriptRef = scriptRef
	}

	return utxo, nil
}

// serializeUTxOs serializes input and output UTxOs into a single byte slice.
func serializeUTxOs(utxosX, utxosY [][]byte) []byte {
	var buf bytes.Buffer

	_ = binary.Write(&buf, binary.LittleEndian, uint64(len(utxosX)))

	for i := 0; i < len(utxosX); i++ {
		_ = binary.Write(&buf, binary.LittleEndian, uint64(len(utxosX[i])))
		buf.Write(utxosX[i])

		_ = binary.Write(&buf, binary.LittleEndian, uint64(len(utxosY[i])))
		buf.Write(utxosY[i])
	}

	return buf.Bytes()
}

func GetTxFromBytes(txBytes []byte) (*Transaction.Transaction, error) {
	tx := &Transaction.Transaction{}
	err := apolloCbor.Unmarshal(txBytes, tx)
	if err != nil {
		return nil, err
	}
	return tx, nil
}

// GetUtxosFromTx extracts UTxOs from transaction bytes using the provided chain context
func GetUtxosFromTx(ctx context.Context, txBytes []byte, chainContext base.ChainContext) ([]apolloUTxO.UTxO, error) {
	tx, err := GetTxFromBytes(txBytes)
	if err != nil {
		return nil, err
	}

	utxos := make([]apolloUTxO.UTxO, 0)

	// Process each input in the transaction
	for _, input := range tx.TransactionBody.Inputs {
		txHash := hex.EncodeToString(input.TransactionId)
		utxo := chainContext.GetUtxoFromRef(txHash, int(input.Index))
		if utxo == nil {
			return nil, fmt.Errorf("UTxO not found for input %s#%d", txHash, input.Index)
		}
		utxos = append(utxos, *utxo)
	}

	return utxos, nil
}

// ParseUTxOsFromJSON parses UTxOs from a JSON file and returns Apollo UTxO objects
func ParseUTxOsFromJSON(jsonData []byte, inputs []TransactionInput.TransactionInput) ([]apolloUTxO.UTxO, error) {
	var txs []UTxOJSON
	if err := json.Unmarshal(jsonData, &txs); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	utxos := make([]apolloUTxO.UTxO, 0)
	for _, input := range inputs {
		inputTxHash := hex.EncodeToString(input.TransactionId)

		for _, tx := range txs {
			for _, output := range tx.Outputs {
				if output.TxHash == inputTxHash && output.OutputIndex == input.Index {
					utxo, err := convertJSONOutputToUTxO(output, input)
					if err != nil {
						return nil, fmt.Errorf("failed to convert output to UTxO: %w", err)
					}
					utxos = append(utxos, utxo)
				}
			}
		}
	}

	return utxos, nil
}

// convertJSONOutputToUTxO converts a JSON output to an Apollo UTxO
func convertJSONOutputToUTxO(output OutputJSON, input TransactionInput.TransactionInput) (apolloUTxO.UTxO, error) {
	addr, err := Address.DecodeAddress(output.Address)
	if err != nil {
		return apolloUTxO.UTxO{}, fmt.Errorf("failed to decode address: %w", err)
	}

	lovelaceAmount := int64(0)
	multiAssets := MultiAsset.MultiAsset[int64]{}

	for _, amt := range output.Amount {
		if amt.Unit == "lovelace" {
			lovelaceAmount = amt.Quantity
			continue
		}

		policyId := Policy.PolicyId{Value: amt.Unit[:56]}
		assetName := *AssetName.NewAssetNameFromHexString(amt.Unit[56:])

		if _, ok := multiAssets[policyId]; !ok {
			multiAssets[policyId] = Asset.Asset[int64]{}
		}
		multiAssets[policyId][assetName] = amt.Quantity
	}

	var txOut TransactionOutput.TransactionOutput
	if output.InlineDatum != "" {
		txOut, err = createAlonzoOutput(addr, lovelaceAmount, multiAssets, output.InlineDatum)
	} else {
		txOut = createShelleyOutput(addr, lovelaceAmount, multiAssets, output.DataHash)
	}

	return apolloUTxO.UTxO{
		Input:  input,
		Output: txOut,
	}, nil
}

// createAlonzoOutput creates a TransactionOutput with Alonzo-era features
func createAlonzoOutput(
	addr Address.Address,
	lovelaceAmount int64,
	multiAssets MultiAsset.MultiAsset[int64],
	inlineDatumHex string,
) (TransactionOutput.TransactionOutput, error) {
	// Create the Value object
	finalValue := createValue(lovelaceAmount, multiAssets)

	// Decode and create the datum
	decoded, err := hex.DecodeString(inlineDatumHex)
	if err != nil {
		return TransactionOutput.TransactionOutput{}, fmt.Errorf("failed to decode inline datum: %w", err)
	}

	var plutusData PlutusData.PlutusData
	if err := apolloCbor.Unmarshal(decoded, &plutusData); err != nil {
		return TransactionOutput.TransactionOutput{}, fmt.Errorf("failed to unmarshal plutus data: %w", err)
	}
	datumOption := PlutusData.DatumOptionInline(&plutusData)

	// Create the Alonzo output
	return TransactionOutput.TransactionOutput{
		IsPostAlonzo: true,
		PostAlonzo: TransactionOutput.TransactionOutputAlonzo{
			Address: addr,
			Amount:  finalValue.ToAlonzoValue(),
			Datum:   &datumOption,
		},
	}, nil
}

// createShelleyOutput creates a TransactionOutput with Shelley-era features
func createShelleyOutput(
	addr Address.Address,
	lovelaceAmount int64,
	multiAssets MultiAsset.MultiAsset[int64],
	datumHashHex string,
) TransactionOutput.TransactionOutput {
	// Create the Value object
	finalValue := createValue(lovelaceAmount, multiAssets)

	// Create datum hash if provided
	datumHash := serialization.DatumHash{}
	if datumHashHex != "" {
		decoded, _ := hex.DecodeString(datumHashHex)
		copy(datumHash.Payload[:], decoded)
	}

	// Create the Shelley output
	return TransactionOutput.TransactionOutput{
		IsPostAlonzo: false,
		PreAlonzo: TransactionOutput.TransactionOutputShelley{
			Address:   addr,
			Amount:    finalValue,
			DatumHash: datumHash,
			HasDatum:  len(datumHash.Payload) > 0,
		},
	}
}

// createValue creates a Value object from lovelace amount and multi-assets
func createValue(lovelaceAmount int64, multiAssets MultiAsset.MultiAsset[int64]) Value.Value {
	if len(multiAssets) > 0 {
		return Value.Value{
			Am: Amount.Amount{
				Coin:  lovelaceAmount,
				Value: multiAssets,
			},
			HasAssets: true,
		}
	}
	return Value.Value{
		Coin:      lovelaceAmount,
		HasAssets: false,
	}
}
