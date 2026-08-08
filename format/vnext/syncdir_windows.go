//go:build windows

package vnext

// Windows does not expose directory fsync through os.File.Sync. The atomic
// hard-link commit still prevents replacement; durable directory flushing is
// delegated to the filesystem on this platform.
func syncDirectory(string) error { return nil }
