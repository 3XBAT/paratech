// Command malgospike is the rejected candidate of the TASK-015 audio capture spike: classic WASAPI loopback of
// the default render endpoint through github.com/gen2brain/malgo (miniaudio, CGO) into closed WAV chunks.
//
// It lives in its own Go module so that the desktop module keeps no CGO dependency; it cannot import the desktop
// module and duplicates a minimal chunk writer. malgo has no process loopback (no INCLUDE or EXCLUDE of a process
// tree), which the owner chose as the primary mode, and it needs a C toolchain in CI.
//
// Build and run on Windows (gcc from MSYS2 mingw64 on PATH):
//
//	GOWORK=off CGO_ENABLED=1 go build -o malgospike.exe .
//	malgospike.exe -out C:\tmp\malgo -duration 10s -chunk 30s
package main
