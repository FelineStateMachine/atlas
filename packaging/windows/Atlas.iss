#ifndef MyAppVersion
  #define MyAppVersion "dev"
#endif
#ifndef MyAppExe
  #define MyAppExe "Atlas.exe"
#endif
#ifndef OutputDir
  #define OutputDir "."
#endif
#ifndef OutputBase
  #define OutputBase "Atlas-setup"
#endif

[Setup]
AppId={{CDE7D85C-293E-4E31-A815-87A08AC04E48}
AppName=Atlas
AppVersion={#MyAppVersion}
AppPublisher=Feline State Machine
DefaultDirName={autopf}\Atlas
DefaultGroupName=Atlas
UninstallDisplayName=Atlas
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog commandline
ChangesAssociations=yes
Compression=lzma2
SolidCompression=yes
OutputDir={#OutputDir}
OutputBaseFilename={#OutputBase}
WizardStyle=modern

[Files]
Source: "{#MyAppExe}"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\Atlas"; Filename: "{app}\Atlas.exe"

[Registry]
Root: HKA; Subkey: "Software\Classes\.atlas"; ValueType: string; ValueName: ""; ValueData: "Atlas.Volume"; Flags: uninsdeletevalue
Root: HKA; Subkey: "Software\Classes\.atlas"; ValueType: string; ValueName: "Content Type"; ValueData: "application/vnd.felinestatemachine.atlas"; Flags: uninsdeletevalue
Root: HKA; Subkey: "Software\Classes\.atlas"; ValueType: string; ValueName: "PerceivedType"; ValueData: "document"; Flags: uninsdeletevalue
Root: HKA; Subkey: "Software\Classes\Atlas.Volume"; ValueType: string; ValueName: ""; ValueData: "Atlas Volume"; Flags: uninsdeletekey
Root: HKA; Subkey: "Software\Classes\Atlas.Volume\DefaultIcon"; ValueType: string; ValueName: ""; ValueData: "{app}\Atlas.exe,0"
Root: HKA; Subkey: "Software\Classes\Atlas.Volume\shell\open\command"; ValueType: string; ValueName: ""; ValueData: """{app}\Atlas.exe"" ""%1"""

[Code]
const
  SHCNE_ASSOCCHANGED = $08000000;
  SHCNF_IDLIST = $0000;

procedure SHChangeNotify(wEventId: LongWord; uFlags: Cardinal; dwItem1, dwItem2: Integer);
  external 'SHChangeNotify@shell32.dll stdcall';

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then
    SHChangeNotify(SHCNE_ASSOCCHANGED, SHCNF_IDLIST, 0, 0);
end;
