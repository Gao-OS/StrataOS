{ pkgs, ... }:

{
  languages.go.enable = true;

  packages = [
    pkgs.gopls
    pkgs.golangci-lint
    pkgs.gotools        # goimports, godoc, etc.
    pkgs.delve          # debugger
    pkgs.go-tools       # staticcheck
  ];

  scripts.strata-build.exec = ''
    echo "Building Strata services..."
    go build -o ./bin/ ./cmd/...
    echo "Built: ./bin/supervisor ./bin/identity ./bin/fs ./bin/strata-ctl"
  '';

  scripts.strata-run.exec = ''
    export STRATA_RUNTIME_DIR="''${STRATA_RUNTIME_DIR:-/tmp/strata}"
    mkdir -p "$STRATA_RUNTIME_DIR"
    echo "Starting Strata supervisor (runtime_dir=$STRATA_RUNTIME_DIR)"
    exec ./bin/supervisor
  '';

  scripts.strata-clean.exec = ''
    rm -rf ./bin /tmp/strata
    echo "Cleaned build artifacts and runtime state"
  '';
}
