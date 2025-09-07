package main

import (
	"fmt"
	"mit6824/lab1-mapreduce/mr"
	"os"
	"plugin"
	_ "unicode"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: mrworker xxx.so\n")
		os.Exit(1)
	}

	mapf, reducef := loadPlugin(os.Args[1])

	mr.Worker(mapf, reducef)
}

// load the application Map and Reduce functions
// from a plugin file, e.g. ../mrapps/wc.so
func loadPlugin(filename string) (func(string, string) []mr.KeyValue, func(string, []string) string) {
	p, err := plugin.Open(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot load plugin %v: %v\n", filename, err)
		os.Exit(1)
	}
	xmapf, err := p.Lookup("Map")
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot find Map in %v: %v\n", filename, err)
		os.Exit(1)
	}
	mapf := xmapf.(func(string, string) []mr.KeyValue)
	xreducef, err := p.Lookup("Reduce")
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot find Reduce in %v: %v\n", filename, err)
		os.Exit(1)
	}
	reducef := xreducef.(func(string, []string) string)

	return mapf, reducef
}
