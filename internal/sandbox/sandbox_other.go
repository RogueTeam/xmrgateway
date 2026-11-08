//go:build !openbsd || !(amd64 || arm64 || riscv64)

package sandbox

const Supported = false

func Unveil(path, permissions string) error { return nil }

func Lock() error { return nil }

func Pledge(promises string) error { return nil }
