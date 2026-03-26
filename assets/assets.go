package assets

import "embed"

//go:embed web/*
var WebAssets embed.FS

//go:embed zip/sounds.tar.gz
var BaresipSounds []byte
