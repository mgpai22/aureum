package aureum

// EvaluatorConfig holds configuration parameters for the Evaluator.
type EvaluatorConfig struct {
	WasmFile     *string // Optional path to custom WASM file
	CostModels   []byte  // Serialized cost models
	MaxTxExSteps uint64  // Maximum transaction execution steps
	MaxTxExMem   uint64  // Maximum transaction execution memory
	ZeroTime     uint64  // Zero time parameter
	ZeroSlot     uint64  // Zero slot parameter
	SlotLength   uint64  // Slot length parameter
}
