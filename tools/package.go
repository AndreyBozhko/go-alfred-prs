package main

import (
	"os"

	"github.com/deanishe/awgo/util/build"
)

func main() {
	if path, err := build.Export(os.Args[0], os.Args[1]); err != nil {
		panic(err)
	} else {
		println("Alfred workflow packaged successfully: " + path)
	}
}
