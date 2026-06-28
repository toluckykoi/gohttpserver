package main

func init() {
	if VERSION != "unknown" {
		return
	}
	VERSION = "v1.0.0"
}
