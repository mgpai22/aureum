package aureum

import _ "embed"

//go:embed wasm/aureum.wasm
var defaultWasmBytes []byte
