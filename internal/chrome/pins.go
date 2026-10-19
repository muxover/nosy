package chrome

// Version is the Chrome for Testing build Nosy installs. To change it, run the
// "Chrome hashes" workflow with the new version and paste its output here.
const Version = "155.0.8059.84"

// pins are the size and SHA-256 of each platform's zip at Version, as printed
// by the "Chrome hashes" workflow from Google's official download URLs. A
// download that differs in either is refused before anything is unpacked.
var pins = map[string]Platform{
	"linux64":   {Size: 199014491, SHA256: "cf0f049892dea22e6a6db26d47ed0d7776b7158edcbec4634086633df8bafcbc"},
	"mac-arm64": {Size: 192672771, SHA256: "13b9e98ce56f5ce18e100a009f455c9285f399e7c9dc8a76fdf2f3a9755bb5f2"},
	"mac-x64":   {Size: 203150281, SHA256: "d805a173118daa5f6f5cf464500789e1e00022a9a6aed8abfec821ab828fa862"},
	"win64":     {Size: 206114888, SHA256: "507f2a031441d10c62b5e3d4e863f09ac1198a571c1419976cbd8387f639afde"},
}
