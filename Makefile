.PHONY: desktop desktop-build check legacy-build legacy-run

desktop:
	npm run tauri dev

desktop-build:
	npm run tauri build

check:
	npx tsc --noEmit
	cargo check --manifest-path src-tauri/Cargo.toml --locked

legacy-build:
	npm run build
	go build -o max-proxy-mock .

legacy-run:
	go run .

legacy-test:
	go test ./...
