package main

import (
	"os"
	"strings"

	"github.com/deanishe/awgo/util/build"
)

func readFile(name string) (text string, err error) {
	content, err := os.ReadFile(name)
	if err != nil {
		return
	}

	text = string(content)
	return
}

func updateInfoPlist(folder string) error {
	txt, err := readFile(folder + "/info.plist")
	if err != nil {
		return err
	}

	version, err := readFile(folder + "/version")
	if err != nil {
		return err
	}

	readme, err := readFile(folder + "/README.md")
	if err != nil {
		return err
	}

	txt = strings.ReplaceAll(txt, "VERSION_PLACEHOLDER", strings.TrimSpace(version))
	txt = strings.ReplaceAll(txt, "README_PLACEHOLDER", strings.TrimSpace(readme))

	return os.WriteFile(folder+"/info.plist", []byte(txt), 0644)

}

func main() {
	src, dest := os.Args[1], os.Args[2]
	if err := updateInfoPlist(src); err != nil {
		panic(err)
	}

	if path, err := build.Export(src, dest); err != nil {
		panic(err)
	} else {
		println("Alfred workflow packaged successfully: " + path)
	}
}
