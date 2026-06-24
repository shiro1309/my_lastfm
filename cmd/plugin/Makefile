PACKAGE = my_lastfm-plugin.ndp

.PHONY: build package clean

build:
	tinygo build -o plugin.wasm -target wasip1 -buildmode=c-shared .

package: build
	zip -j $(PACKAGE) manifest.json plugin.wasm

clean:
	rm -f plugin.wasm $(PACKAGE)