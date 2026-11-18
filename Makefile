.PHONY: run test dataset review testzip classify-testzip classify-decisions classify-openai docker-up docker-down clean

run:
	go run ./cmd/photo-clean

test:
	go test ./...

dataset:
	python3 scripts/fetch_dataset.py --per-class 150

review:
	python3 scripts/review.py --report out/informe-decisions.json --out out/revision.html

testzip:
	python3 scripts/make_test_zip.py --per-class 25 --out out/e2e-50 --zip out/e2e-50.zip

classify-testzip:
	go run ./cmd/classify -in out/e2e-50 -provider decisions -out out/informe-e2e-50.json

classify-decisions:
	go run ./cmd/classify -in dataset -provider decisions -out out/informe-decisions.json

classify-openai:
	go run ./cmd/classify -in dataset -provider openai -out out/informe-openai.json

docker-up:
	docker compose up -d

docker-down:
	docker compose down

clean:
	rm -rf out
