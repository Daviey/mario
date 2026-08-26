//go:build js

package persist

import "syscall/js"

// The browser has no filesystem: the player identity (device UUID + name
// + local best) lives in localStorage under one key. Without this store
// every web submission carries device_id "" and the server's uuid column
// rejects it with 22P02 — the "SUBMIT FAILED" web bug. Storage access can
// throw in hardened privacy modes, so every call is recover()ed: identity
// then degenerates to fresh-per-load, which still plays fine.

const playerStoreKey = "mario.player"

func loadPlayerBytes() (data []byte) {
	defer func() { recover() }()
	v := js.Global().Get("localStorage").Call("getItem", playerStoreKey)
	if !v.Truthy() {
		return nil
	}
	return []byte(v.String())
}

func storePlayerBytes(data []byte) {
	defer func() { recover() }()
	js.Global().Get("localStorage").Call("setItem", playerStoreKey, string(data))
}
