"""Static acceptance for the native packages produced by release.yml."""

from pathlib import Path
import plistlib
import subprocess
import tempfile
import unittest
import xml.etree.ElementTree as ET


ROOT = Path(__file__).resolve().parents[2]
MIME = "application/vnd.felinestatemachine.atlas"
EXTENSION = "atlas"


class NativePackagingTest(unittest.TestCase):
    def test_macos_exports_atlas_document_type(self) -> None:
        with (ROOT / "packaging/macos/Info.plist").open("rb") as source:
            info = plistlib.load(source)

        self.assertEqual(info["CFBundleIdentifier"], "dev.felinestatemachine.atlas")
        self.assertTrue(info["LSSupportsOpeningDocumentsInPlace"])
        declaration = info["UTExportedTypeDeclarations"][0]
        self.assertIn(EXTENSION, declaration["UTTypeTagSpecification"]["public.filename-extension"])
        self.assertEqual(declaration["UTTypeTagSpecification"]["public.mime-type"], MIME)
        self.assertIn(declaration["UTTypeIdentifier"], info["CFBundleDocumentTypes"][0]["LSItemContentTypes"])

    def test_linux_package_registers_atlas_mime(self) -> None:
        desktop = (ROOT / "packaging/linux/dev.felinestatemachine.Atlas.desktop").read_text()
        self.assertIn(f"MimeType={MIME};", desktop)
        self.assertIn("Exec=Atlas %f", desktop)
        self.assertIn("Terminal=false", desktop)

        mime = ET.parse(ROOT / "packaging/linux/dev.felinestatemachine.atlas.xml").getroot()
        namespace = {"mime": "http://www.freedesktop.org/standards/shared-mime-info"}
        mime_type = mime.find("mime:mime-type", namespace)
        self.assertIsNotNone(mime_type)
        self.assertEqual(mime_type.attrib["type"], MIME)
        glob = mime_type.find("mime:glob", namespace)
        self.assertEqual(glob.attrib["pattern"], f"*.{EXTENSION}")

        control = (ROOT / "packaging/linux/control").read_text()
        self.assertIn("Package: atlas-desktop", control)
        self.assertIn("Depends: libgtk-3-0, libwebkit2gtk-4.1-0", control)

    def test_windows_installer_registers_atlas_file(self) -> None:
        installer = (ROOT / "packaging/windows/Atlas.iss").read_text()
        self.assertIn("ChangesAssociations=yes", installer)
        self.assertIn("PrivilegesRequired=lowest", installer)
        self.assertIn("Software\\Classes\\.atlas", installer)
        self.assertIn('ValueData: "Atlas.Volume"', installer)
        self.assertIn(f'ValueData: "{MIME}"', installer)
        self.assertIn('ValueData: """{app}\\Atlas.exe"" ""%1"""', installer)
        self.assertIn("SHChangeNotify", installer)

    def test_release_is_draft_first_and_verifies_content(self) -> None:
        workflow = (ROOT / ".github/workflows/release.yml").read_text()
        self.assertIn("scripts/release/verify-assets.sh", workflow)
        self.assertIn("--draft", workflow)
        self.assertIn("--draft=false", workflow)
        self.assertIn(".digest", workflow)
        self.assertIn("package-desktop.sh", workflow)
        self.assertIn("sign-windows.ps1", workflow)
        self.assertIn("package-smoke.sh", workflow)
        self.assertIn("smoke-macos.sh", workflow)
        self.assertIn("smoke-linux.sh", workflow)
        self.assertIn("smoke-windows.ps1", workflow)
        self.assertNotIn("& $installer /VERYSILENT", workflow)
        mac_package = (ROOT / "scripts/release/package-desktop.sh").read_text()
        self.assertIn("sign-macos.sh", mac_package)
        self.assertIn("stage=$(mktemp -d)", mac_package)
        self.assertNotIn('app="$root/Atlas.app"', mac_package)

        mac_signer = (ROOT / "scripts/release/sign-macos.sh").read_text()
        self.assertIn('--keychain "$keychain"', mac_signer)
        self.assertNotIn("security list-keychains", mac_signer)
        self.assertIn('xcrun stapler validate "$app"', mac_signer)
        self.assertIn('codesign --force --deep --sign - "$app"', mac_signer)

        windows_smoke = (ROOT / "scripts/release/smoke-windows.ps1").read_text()
        self.assertIn("Start-Process -FilePath $Installer", windows_smoke)
        self.assertIn("$install.ExitCode", windows_smoke)
        self.assertIn("application/vnd.felinestatemachine.atlas", windows_smoke)
        linux_smoke = (ROOT / "scripts/release/smoke-linux.sh").read_text()
        self.assertIn("dpkg-query", linux_smoke)
        self.assertIn("xdg-mime query filetype", linux_smoke)
        self.assertIn("ATLAS_BUNDLES_DIR", linux_smoke)
        mac_smoke = (ROOT / "scripts/release/smoke-macos.sh").read_text()
        self.assertIn("codesign --verify --deep --strict", mac_smoke)
        self.assertIn("dev.felinestatemachine.atlas.volume", mac_smoke)
        self.assertIn("ATLAS_BUNDLES_DIR", mac_smoke)

    def test_release_asset_allowlist_is_exact(self) -> None:
        version = "v1.2.3"
        assets = [
            f"Atlas-{version}-darwin-arm64.zip",
            f"Atlas-{version}-linux-amd64.deb",
            f"Atlas-{version}-windows-amd64-setup.exe",
            f"atlas-cli-{version}-darwin-amd64.tar.gz",
            f"atlas-cli-{version}-darwin-arm64.tar.gz",
            f"atlas-cli-{version}-linux-amd64.tar.gz",
            f"atlas-cli-{version}-linux-arm64.tar.gz",
            f"atlas-cli-{version}-windows-amd64.zip",
        ]
        with tempfile.TemporaryDirectory() as directory:
            dist = Path(directory)
            for asset in assets:
                (dist / asset).touch()

            verifier = ROOT / "scripts/release/verify-assets.sh"
            subprocess.run([verifier, dist, version], check=True, capture_output=True)
            self.assertEqual(len((dist / "SHA256SUMS").read_text().splitlines()), len(assets))

            (dist / "unexpected.zip").touch()
            rejected = subprocess.run([verifier, dist, version], capture_output=True)
            self.assertNotEqual(rejected.returncode, 0)
            self.assertIn(b"unexpected.zip", rejected.stdout + rejected.stderr)


if __name__ == "__main__":
    unittest.main()
