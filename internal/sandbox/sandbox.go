// Package sandbox wraps OpenBSD's unveil(2) and pledge(2) without
// golang.org/x/sys. On other systems every call is a no-op.
package sandbox
