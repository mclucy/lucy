{
  description = "lucy — The Minecraft server package manager";

  inputs.nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";

  outputs =
    {
      self,
      nixpkgs,
    }:
    let
      lib = nixpkgs.lib;

      # nixpkgs 26.11 dropped x86_64-darwin, so Intel macOS has no packages to build.
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "aarch64-darwin"
      ];

      forAllSystems = lib.genAttrs systems;

      # go.mod names the toolchain this source needs. A nixpkgs channel can carry
      # an older default `go`, so select the matching versioned derivation and
      # fall back to the default one when nixpkgs has no such attribute.
      goDirective = lib.findFirst (line: lib.hasPrefix "go " line) "go 1.0" (
        lib.splitString "\n" (builtins.readFile ./go.mod)
      );
      goMajorMinor = lib.versions.majorMinor (lib.removePrefix "go " goDirective);
      goPackage = pkgs: pkgs."go_${lib.replaceStrings [ "." ] [ "_" ] goMajorMinor}" or pkgs.go;
      buildGoModule =
        pkgs: pkgs."buildGo${lib.replaceStrings [ "." ] [ "" ] goMajorMinor}Module" or pkgs.buildGoModule;

      # The repository is pre-release, so a source build identifies itself by revision.
      revision = self.dirtyShortRev or self.shortRev or "dev";

      mkLucy =
        pkgs:
        (buildGoModule pkgs) {
          pname = "lucy";
          version = revision;

          src = ./.;

          vendorHash = "sha256-IYVsW1OVWHtNvkt2q4QpwJKSZrpcKoQSetjIr6o94/A=";

          # The suite needs a writable HOME and the network; CI runs it before the
          # release build instead.
          doCheck = false;

          # Mirrors the release build: no cgo, and HTTP/2 dropped from net/http.
          env.CGO_ENABLED = "0";
          tags = [
            "release"
            "nethttpomithttp2"
          ];

          # -s -w drops more than stdenv's post-link strip, which shrinks the binary.
          ldflags = [
            "-s"
            "-w"
            "-X github.com/mclucy/lucy/cmd.version=${revision}"
            "-X github.com/mclucy/lucy/cmd.commit=${revision}"
          ];

          nativeBuildInputs = [ pkgs.installShellFiles ];

          postInstall = ''
            # The binary writes its log under HOME, which the build sandbox leaves read-only.
            export HOME="$TMPDIR"
            installShellCompletion --cmd lucy \
              --bash <($out/bin/lucy completion bash) \
              --zsh <($out/bin/lucy completion zsh) \
              --fish <($out/bin/lucy completion fish)
          '';

          meta = {
            description = "The Minecraft server package manager";
            homepage = "https://github.com/mclucy/lucy";
            license = lib.licenses.asl20;
            mainProgram = "lucy";
            platforms = systems;
          };
        };

      mkDevShell =
        pkgs:
        pkgs.mkShell {
          packages = [
            (goPackage pkgs)
            pkgs.gopls
            pkgs.golangci-lint
            pkgs.go-task
            pkgs.watchexec
            pkgs.nixfmt
            pkgs.codegraph
            pkgs.git
          ];

          # Tools installed with `go install` land in the default GOPATH bin.
          shellHook = ''
            export PATH="$HOME/go/bin:$PATH"
          '';
        };

      # NixOS and home-manager differ only in the list that receives the package.
      mkModule =
        install:
        {
          config,
          lib,
          pkgs,
          ...
        }:
        let
          cfg = config.programs.lucy;
        in
        {
          options.programs.lucy = {
            enable = lib.mkEnableOption "lucy - The Minecraft server package manager";
            package = lib.mkOption {
              type = lib.types.package;
              default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
              description = "The lucy package to install.";
            };
          };

          config = lib.mkIf cfg.enable (install cfg.package);
        };
    in
    {
      packages = forAllSystems (system: {
        default = mkLucy nixpkgs.legacyPackages.${system};
      });

      checks = forAllSystems (system: {
        default = self.packages.${system}.default;
      });

      devShells = forAllSystems (system: {
        default = mkDevShell nixpkgs.legacyPackages.${system};
      });

      formatter = forAllSystems (system: nixpkgs.legacyPackages.${system}.nixfmt);

      nixosModules.default = mkModule (package: {
        environment.systemPackages = [ package ];
      });
      homeManagerModules.default = mkModule (package: {
        home.packages = [ package ];
      });
    };
}
