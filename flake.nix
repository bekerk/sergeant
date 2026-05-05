{
  description = "sergeant - Slack debt-tracking bot";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
          config.allowUnfreePredicate = pkg:
            builtins.elem (nixpkgs.lib.getName pkg) [ "ngrok" ];
        };

        sergeant = pkgs.buildGoModule {
          pname = "sergeant";
          version = "0.1.0";

          src = pkgs.lib.cleanSourceWith {
            src = ./.;
            filter = path: type:
              let base = baseNameOf (toString path);
              in !(pkgs.lib.hasSuffix ".db" base)
                && !(pkgs.lib.hasPrefix "result" base);
          };

          vendorHash = "sha256-H1IzQHCiCteOuWWHUEUiBMqtluXzoSF+Bmp8EUH8yE8=";

          subPackages = [ "cmd/sergeant" ];
          ldflags = [ "-s" "-w" ];

          doCheck = true;

          meta = with pkgs.lib; {
            description = "Slack bot that tracks per-user debts via @mention commands";
            homepage = "https://github.com/fourtheon/sergeant";
            license = licenses.mit;
            mainProgram = "sergeant";
            platforms = platforms.unix;
          };
        };

        sergeant-pl = pkgs.symlinkJoin {
          name = "sergeant-pl-${sergeant.version}";
          paths = [ sergeant ];
          nativeBuildInputs = [ pkgs.makeWrapper ];
          postBuild = ''
            wrapProgram $out/bin/sergeant --set-default SERGEANT_LOCALE pl
          '';
          inherit (sergeant) meta;
        };
      in
      {
        packages = {
          default = sergeant;
          sergeant = sergeant;
          sergeant-pl = sergeant-pl;
        };

        apps.default = {
          type = "app";
          program = "${sergeant}/bin/sergeant";
          meta = sergeant.meta;
        };

        apps.sergeant-pl = {
          type = "app";
          program = "${sergeant-pl}/bin/sergeant";
          meta = sergeant.meta;
        };

        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            ngrok
            go
            gopls
            gotools
            go-tools
            golangci-lint
            sqlite
          ];
        };

        checks = {
          build = sergeant;
          fmt = pkgs.runCommand "check-nixpkgs-fmt"
            { nativeBuildInputs = [ pkgs.nixpkgs-fmt ]; } ''
            ${pkgs.nixpkgs-fmt}/bin/nixpkgs-fmt --check ${./flake.nix}
            touch $out
          '';
        };

        formatter = pkgs.nixpkgs-fmt;
      });
}
