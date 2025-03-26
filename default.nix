{buildGoModule}:
buildGoModule {
  pname = "probe";
  version = "0.0.0";

  src = ./.;

  vendorHash = "sha256-tI+PM+K5yBrwomC9hFxduwEdAKb1vvK+k4T6hZKwK8k=";

  doCheck = false;

  meta.mainProgram = "probe";
}
