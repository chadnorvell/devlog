{
  description = "devlog - daily work summary generator";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      supportedSystems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = nixpkgs.lib.genAttrs supportedSystems;
    in
    {
      packages = forAllSystems (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          default = pkgs.buildGoModule {
            pname = "devlog";
            version = "0.1.0";
            src = ./.;
            vendorHash = "sha256-pbA/AlBz3cQYRTMnQ/qBPcinYOKokrBLNhkbRTq54gE=";
            nativeCheckInputs = [ pkgs.git ];
            preCheck = ''
              export HOME=$TMPDIR
              git config --global user.email "test@test.com"
              git config --global user.name "Test"
            '';
          };
        });

      devShells = forAllSystems (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          default = pkgs.mkShell {
            buildInputs = with pkgs; [
              go
              git
            ];
          };
        });
    };
}
