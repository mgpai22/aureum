# Aureum

Aureum is a GO library for building and evaluating Cardano Plutus scripts.

It leverages [wasm](https://github.com/mgpai22/aureum-rust) to use the [uplc](https://crates.io/crates/uplc) crate. 

For executing wasm, this makes use of [wazero](https://github.com/tetratelabs/wazero), the zero dependency WebAssembly runtime for Go developers.

# Usage
See the [example](./example) folder for more details.


You can get the latest version of aureum like this.

```bash
go get github.com/mgpai22/aureum@latest
```