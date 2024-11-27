package aureum

type EvalError struct {
	ErrorType  string   `cbor:"error_type"`
	Budget     Budget   `cbor:"budget"`
	DebugTrace []string `cbor:"debug_trace"`
}

type Budget struct {
	Mem uint64 `cbor:"mem"`
	CPU uint64 `cbor:"cpu"`
}

type UTxO struct {
	Address     string            `json:"address"`
	TxHash      string            `json:"tx_hash"`
	OutputIndex uint64            `json:"output_index"`
	DatumHash   *string           `json:"datum_hash,omitempty"`
	Datum       *string           `json:"datum,omitempty"`
	ScriptRef   *ScriptRef        `json:"script_ref,omitempty"`
	Assets      map[string]uint64 `json:"assets"`
}

type ScriptRef struct {
	ScriptType string `json:"script_type"`
	Script     string `json:"script"`
}

type UTxOJSON struct {
	Hash    string       `json:"hash"`
	Outputs []OutputJSON `json:"outputs"`
}

type OutputJSON struct {
	TxHash      string      `json:"tx_hash"`
	OutputIndex int         `json:"output_index"`
	Address     string      `json:"address"`
	Amount      []AssetJSON `json:"amount"`
	InlineDatum string      `json:"inline_datum"`
	DataHash    string      `json:"data_hash"`
}

type AssetJSON struct {
	Unit     string `json:"unit"`
	Quantity int64  `json:"quantity"`
}
