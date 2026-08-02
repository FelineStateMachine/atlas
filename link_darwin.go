//go:build darwin && cgo

package main

// Wails' macOS file dialog builds its allowed-file-type list out of UTType,
// and declares the class without asking the linker for the framework it lives
// in. Without this the desktop build fails at link with an undefined
// _OBJC_CLASS_$_UTType, so the one line that fixes it lives here rather than
// in a build-flag incantation somebody has to remember.

/*
#cgo LDFLAGS: -framework UniformTypeIdentifiers
*/
import "C"
