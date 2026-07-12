package main

func init() {
	// When the binary is built without the build script's -ldflags
	// injection, VERSION stays at "unknown". Previously we overwrote
	// that with a hardcoded "v1.0.0", which made /-/sysinfo report a
	// misleading version unrelated to the actual build. Falling back to
	// "dev" makes it obvious this is an untagged development build.
	if VERSION == "unknown" {
		VERSION = "dev"
	}
}
