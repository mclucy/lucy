{
  description = "lucy — The Minecraft server package manager";

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
            enable = mkEnableOption "lucy — The Minecraft server package manager";
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
            enable = mkEnableOption "lucy — The Minecraft server package manager";
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
        packages.godoc-mcp = pkgs.buildGoModule {
          pname = "godoc-mcp";
          version = "latest";

          src = pkgs.fetchFromGitHub {
            owner = "mrjoshuak";
            repo = "godoc-mcp";
            rev = "v1.1.0";
            hash = "sha256-0cn0QLmVcjzA62oeKeHcEqeckk3CoxeM9UOuowPe17c=";
          };

          vendorHash = "sha256-N/ZV0FWKigMGUYyyOQHIrmvvcd83NQ0OAxiS1ExMoUw=";

          doCheck = false;

          meta = with pkgs.lib; {
            description = "MCP server for efficient Go documentation access";
            homepage = "https://github.com/mrjoshuak/godoc-mcp";
            license = licenses.mit;
            mainProgram = "godoc-mcp";
          };
        };

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
            description = "The Minecraft server package manager";
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
            codegraph
            gopls
            self.packages.${system}.godoc-mcp
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
