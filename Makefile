.PHONY: generate serve clean build bench solution pgo test-labs

# Website ------------------------------------------------------------------
#
# Modules are listed (and enabled/disabled) in exercises/modules.json.
# ONLY and SKIP override that per build, e.g.:
#   make generate SKIP=12,17
#   make serve ONLY=00,01,02,03

GENERATOR_FLAGS = -exercises ../exercises -images ../images -output ../website -only "$(ONLY)" -skip "$(SKIP)"

generate:
	cd website-generator && go run . $(GENERATOR_FLAGS)

serve:
	cd website-generator && go run . $(GENERATOR_FLAGS) -serve

build:
	cd website-generator && go build -o ../bin/generator .

clean:
	rm -rf website/

# Labs ---------------------------------------------------------------------
#
#   make bench LAB=03            benchmark your after.go against before.go
#   make solution LAB=03         same, but with the reference solution.go
#   make bench LAB=03 COUNT=10   more runs for less noisy results
#   make bench LAB=15 CPU=1,4,16 run with several GOMAXPROCS values
#   make pgo                     module 18: benchmark without PGO, collect a
#                                CPU profile, rebuild with it and compare
#   make test-labs               check that every lab builds and passes tests

COUNT ?= 6
BENCH ?= .

bench:
	@test -n "$(LAB)" || (echo "usage: make bench LAB=NN" && exit 1)
	cd labs && go test -run='^$$' -bench='$(BENCH)' -benchmem -count=$(COUNT) $(if $(CPU),-cpu=$(CPU)) $(TAGS) ./$(LAB)-*/ | tee $(LAB).bench.txt
	benchstat -col /impl labs/$(LAB).bench.txt

solution:
	@$(MAKE) --no-print-directory bench LAB=$(LAB) COUNT=$(COUNT) BENCH='$(BENCH)' CPU=$(CPU) TAGS="-tags solution"

pgo:
	cd labs && go test -pgo=off -run='^$$' -bench=Ingest -benchmem -count=$(COUNT) ./18-pgo/ | tee 18-off.bench.txt
	cd labs && go test -pgo=off -run='^$$' -bench=Ingest -cpuprofile=18-cpu.prof ./18-pgo/ > /dev/null
	cd labs && go test -pgo=18-cpu.prof -run='^$$' -bench=Ingest -benchmem -count=$(COUNT) ./18-pgo/ | tee 18-pgo.bench.txt
	benchstat labs/18-off.bench.txt labs/18-pgo.bench.txt
	rm -f labs/18-pgo.test

test-labs:
	cd labs && go vet ./... && go vet -tags solution ./...
	cd labs && go test ./... && go test -tags solution ./...
