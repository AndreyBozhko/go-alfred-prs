package main

import (
	"github.com/deanishe/awgo/util/build"
)

func main() {
	if path, err := build.Export("build", "dist"); err != nil {
		panic(err)
	} else {
		println("SUCCESS: " + path)
	}
}
