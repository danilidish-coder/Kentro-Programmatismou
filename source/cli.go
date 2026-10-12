//go:build !windows

package main

import (
	"fmt"
	"kentro.local/programmatismou/engine"
)

func startApp() {
	fmt.Println("Κέντρο Προγραμματισμού", engine.ProductVersion, "— το γραφικό περιβάλλον εκτελείται σε Windows")
}
