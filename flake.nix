{
  description = "terraform-provider-supabase-vault - development environment";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }: let
    supported = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
    forAllSystems = nixpkgs.lib.genAttrs supported;
    # Terraform 1.14+ is BSL-licensed; allow it explicitly.
    pkgsFor = system: import nixpkgs {
      inherit system;
      config.allowUnfreePredicate = pkg:
        (nixpkgs.lib.getName pkg) == "terraform" ||
        nixpkgs.lib.hasPrefix "terraform-" (nixpkgs.lib.getName pkg);
    };
  in {
    devShells = forAllSystems (system: let
      pkgs = pkgsFor system;
    in {
      default = pkgs.mkShell {
        name = "terraform-provider-supabase-vault-dev";

        packages = [
          pkgs.pre-commit
          pkgs.terraform
          pkgs.opentofu 
          pkgs.tflint
          pkgs.golangci-lint
        ];

        shellHook = ''
          echo "Terraform:  $(terraform version)"
          echo "Tofu:       $(tofu version)"
          echo "Pre-commit: $(pre-commit --version)"
          echo ""
          if [ -d .git ]; then
            pre-commit install 2>/dev/null || true
            # Run pre-commit inside nix develop so Cursor's git client (which
            # doesn't load this dev shell) still has tofu/tflint on PATH.
            root="$(git rev-parse --show-toplevel)"
            printf '%s\n' '#!/bin/sh' 'cd "$(git rev-parse --show-toplevel)" && nix develop -c pre-commit run --hook-stage pre-commit' > "$root/.git/hooks/pre-commit"
            chmod +x "$root/.git/hooks/pre-commit"
          fi
        '';
      };
    });
  };
}
