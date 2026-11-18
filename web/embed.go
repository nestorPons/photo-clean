// Package web contiene las plantillas HTML y los assets estáticos que se
// incrustan en el binario del servidor.
package web

import "embed"

// Assets agrupa templates/ y static/.
//
//go:embed templates static
var Assets embed.FS
