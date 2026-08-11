{
  description = "Nix flake for Surge - blazing fast TUI download manager";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = {
    self,
    nixpkgs,
  }: let
    version = "0.11.2";
    systems = ["x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin"];
    forEachSystem = f:
      nixpkgs.lib.genAttrs systems (
        system:
          f nixpkgs.legacyPackages.${system}
      );
  in {
    packages = forEachSystem (pkgs: rec {
      surge = pkgs.callPackage ./package.nix {
        src = self;
        inherit version;
      };
      default = surge;
    });

    overlays.default = final: _prev: {
      surge = final.callPackage ./package.nix {
        src = self;
        inherit version;
      };
    };

    nixosModules.default = {
      lib,
      pkgs,
      config,
      ...
    }: let
      cfg = config.programs.surge;
    in {
      options.programs.surge = {
        enable = lib.mkEnableOption "surge download manager";
        systemd.enable = lib.mkEnableOption "systemd service";
      };

      config = lib.mkIf cfg.enable {
        nixpkgs.overlays = [self.overlays.default];
        environment.systemPackages = [pkgs.surge];
        systemd.services.surge = lib.mkIf cfg.systemd.enable {
          description = "Surge Download Manager";

          wantedBy = ["multi-user.target"];
          after = ["network-online.target"];
          wants = ["network-online.target"];

          serviceConfig = {
            ExecStart = "${pkgs.surge}/bin/surge server start --is-system-service";

            Restart = "on-failure";
            RestartSec = "5s";
          };
        };
      };
    };
  };
}
