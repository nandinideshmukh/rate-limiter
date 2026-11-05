package web

import _ "embed"

//go:embed index.html
// means that the contents of the index.html file will be embedded into the Go binary at compile time. The variable IndexHTML will hold the byte slice representation of the contents of index.html, allowing it to be accessed directly in the code without needing to read from the file system.
var IndexHTML []byte
