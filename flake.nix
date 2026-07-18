{
  description = "Lucy — Minecraft server package manager";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    let
      # ── NixOS module ────────────────────────────────────────
      nixosModule =
        {
          config,
          lib,
          pkgs,
          ...
        }:
        with lib;
        let
          cfg = config.programs.lucy;
        in
        {
          options.programs.lucy = {
            enable = mkEnableOption "Lucy — Minecraft server package manager";
            package = mkOption {
              type = types.package;
              default = self.packages.${pkgs.system}.default;
              description = "lucy package to use";
            };
          };
          config = mkIf cfg.enable {
            environment.systemPackages = [ cfg.package ];
          };
        };

      # ── Home Manager module ─────────────────────────────────
      homeManagerModule =
        {
          config,
          lib,
          pkgs,
          ...
        }:
        with lib;
        let
          cfg = config.programs.lucy;
        in
        {
          options.programs.lucy = {
            enable = mkEnableOption "Lucy — Minecraft server package manager";
            package = mkOption {
              type = types.package;
              default = self.packages.${pkgs.system}.default;
              description = "lucy package to use";
            };
          };
          config = mkIf cfg.enable {
            home.packages = [ cfg.package ];
          };
        };
    in
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs { inherit system; };
      in
      {
        packages.default = pkgs.buildGoModule {
          pname = "lucy";
          version =
            builtins.substring 0 8 (self.lastModifiedDate or "dev") + "-" + (self.shortRev or "dirty");

          src = ./.;
          vendorHash = "sha256-NHGdBG5HpX+MYX5GDLXn55fp1OI26//7V9blw8z++P0=";

          nativeBuildInputs = [ pkgs.installShellFiles ];

          ldflags = [
            "-s -w"
            "-X github.com/mclucy/lucy/cmd.version=${self.lastModifiedDate or "dev"}"
            "-X github.com/mclucy/lucy/cmd.commit=${self.shortRev or "dirty"}"
          ];

          tags = [ "release" ];

          postInstall = ''
            installShellCompletion --cmd lucy \
              --bash <($out/bin/lucy completion bash) \
              --zsh <($out/bin/lucy completion zsh) \
              --fish <($out/bin/lucy completion fish)
          '';

          meta = with pkgs.lib; {
            description = "Minecraft server package manager";
            homepage = "https://github.com/mclucy/lucy";
            license = licenses.asl20;
            mainProgram = "lucy";
          };
        };

        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            go
            gopls
            golangci-lint
            go-task
            watchexec
            nixfmt
          ];

          shellHook = ''
            export GOPATH="$HOME/go"
            export PATH="$GOPATH/bin:$PATH"
          '';
        };

        formatter = pkgs.nixfmt;
      }
    )
    // {
      nixosModules.default = nixosModule;
      homeManagerModules.default = homeManagerModule;
    };
}
