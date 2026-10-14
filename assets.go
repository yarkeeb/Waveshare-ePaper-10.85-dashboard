package assets

import "embed"

// Files contains the original dashboard's fonts and bitmap icons.
//
//go:embed fnt/src/*.ttf icons/*.bmp
var Files embed.FS
