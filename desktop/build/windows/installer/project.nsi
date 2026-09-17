Unicode true

####
## Please note: Template replacements don't work in this file. They are provided with default defines like
## mentioned underneath.
## If the keyword is not defined, "wails_tools.nsh" will populate them with the values from ProjectInfo.
## If they are defined here, "wails_tools.nsh" will not touch them. This allows to use this project.nsi manually
## from outside of Wails for debugging and development of the installer.
##
## For development first make a wails nsis build to populate the "wails_tools.nsh":
## > wails build --target windows/amd64 --nsis
## Then you can call makensis on this file with specifying the path to your binary:
## For a AMD64 only installer:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app.exe
## For a ARM64 only installer:
## > makensis -DARG_WAILS_ARM64_BINARY=..\..\bin\app.exe
## For a installer with both architectures:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app-amd64.exe -DARG_WAILS_ARM64_BINARY=..\..\bin\app-arm64.exe
####
## The following information is taken from the ProjectInfo file, but they can be overwritten here.
####
## !define INFO_PROJECTNAME    "MyProject" # Default "{{.Name}}"
## !define INFO_COMPANYNAME    "MyCompany" # Default "{{.Info.CompanyName}}"
## !define INFO_PRODUCTNAME    "MyProduct" # Default "{{.Info.ProductName}}"
## !define INFO_PRODUCTVERSION "1.0.0"     # Default "{{.Info.ProductVersion}}"
## !define INFO_COPYRIGHT      "Copyright" # Default "{{.Info.Copyright}}"
###
## !define PRODUCT_EXECUTABLE  "Application.exe"      # Default "${INFO_PROJECTNAME}.exe"
## !define UNINST_KEY_NAME     "UninstKeyInRegistry"  # Default "${INFO_COMPANYNAME}${INFO_PRODUCTNAME}"
####
## Per-user install under %LOCALAPPDATA%\CZL\<product>: no UAC, nothing in Program Files.
## wails_tools.nsh is regenerated on every build, so every override has to live in this file.
!define REQUEST_EXECUTION_LEVEL "user"
####
## Include the wails tools
####
!include "wails_tools.nsh"

# The version information for this two must consist of 4 parts
VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"

VIAddVersionKey "CompanyName"     "CZL"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

# Enable HiDPI support. https://nsis.sourceforge.io/Reference/ManifestDPIAware
ManifestDPIAware true

!include "MUI.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
!define MUI_FINISHPAGE_NOAUTOCLOSE # Wait on the INSTFILES page so the user can take a look into the details of the installation steps
!define MUI_ABORTWARNING # This will warn the user if they exit from the installer.

!insertmacro MUI_PAGE_WELCOME # Welcome to the installer page.
# No directory page: the install root is fixed, and the app resolves it from %LOCALAPPDATA% at runtime anyway.
!insertmacro MUI_PAGE_INSTFILES # Installing page.
!insertmacro MUI_PAGE_FINISH # Finished installation page.

!insertmacro MUI_UNPAGE_INSTFILES # Uinstalling page

!insertmacro MUI_LANGUAGE "English" # Set the Language of the installer

## The following two statements can be used to sign the installer and the uninstaller. The path to the binaries are provided in %1
#!uninstfinalize 'signtool --file "%1"'
#!finalize 'signtool --file "%1"'

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe" # Name of the installer's file.
InstallDir "$LOCALAPPDATA\CZL\${INFO_PRODUCTNAME}"
ShowInstDetails show # This will always show the installation details.

# Uninstall entry goes to HKCU: wails.writeUninstaller writes HKLM, which fails silently without admin.
!macro czl.writeUninstaller
    WriteUninstaller "$INSTDIR\uninstall.exe"

    WriteRegStr HKCU "${UNINST_KEY}" "Publisher" "CZL"
    WriteRegStr HKCU "${UNINST_KEY}" "DisplayName" "${INFO_PRODUCTNAME}"
    WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion" "${INFO_PRODUCTVERSION}"
    WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
    WriteRegStr HKCU "${UNINST_KEY}" "UninstallString" "$\"$INSTDIR\uninstall.exe$\""
    WriteRegStr HKCU "${UNINST_KEY}" "QuietUninstallString" "$\"$INSTDIR\uninstall.exe$\" /S"
    WriteRegDWORD HKCU "${UNINST_KEY}" "NoModify" 1
    WriteRegDWORD HKCU "${UNINST_KEY}" "NoRepair" 1

    ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
    IntFmt $0 "0x%08X" $0
    WriteRegDWORD HKCU "${UNINST_KEY}" "EstimatedSize" "$0"
!macroend

Function .onInit
    !insertmacro wails.checkArchitecture

    # Older releases installed for all users under Program Files. Removing that needs admin,
    # so only offer to launch its own uninstaller. User data is not touched by it and is
    # migrated into the new layout on first start.
    SetRegView 64
    ReadRegStr $0 HKLM "${UNINST_KEY}" "UninstallString"
    ${If} $0 != ""
        MessageBox MB_YESNO|MB_ICONQUESTION "An older ${INFO_PRODUCTNAME} is installed for all users under Program Files.$\r$\n$\r$\nRun its uninstaller now? Your data is kept either way." /SD IDNO IDNO skipLegacy
        ExecWait $0
    skipLegacy:
    ${EndIf}
FunctionEnd

# /relaunch is passed by the app's auto-update: start the new version once files are in place.
Function .onInstSuccess
    ${GetParameters} $R0
    ClearErrors
    ${GetOptions} $R0 "/relaunch" $R1
    IfErrors +2
    Exec '"$INSTDIR\${PRODUCT_EXECUTABLE}"'
FunctionEnd

Section
    !insertmacro wails.setShellContext

    !insertmacro wails.webview2runtime

    SetOutPath $INSTDIR

    # Auto-update starts this installer and then quits the app; wait until its exe is released.
    # Also covers a manual install while the app is still open.
    StrCpy $1 0
    waitExe:
    ClearErrors
    Delete "$INSTDIR\${PRODUCT_EXECUTABLE}"
    IfErrors 0 exeReleased
    IntOp $1 $1 + 1
    IntCmp $1 40 exeBusy 0 exeBusy
    Sleep 500
    Goto waitExe
    exeBusy:
    MessageBox MB_OK|MB_ICONSTOP "${INFO_PRODUCTNAME} is still running. Close it and run the installer again." /SD IDOK
    Abort
    exeReleased:

    !insertmacro wails.files

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    !insertmacro czl.writeUninstaller
SectionEnd

Section "uninstall"
    !insertmacro wails.setShellContext

    # Program files and rebuildable dirs go; data\ only if the user says so (default: keep).
    # Never RMDir /r $INSTDIR: data\ lives inside it.
    Delete "$INSTDIR\${PRODUCT_EXECUTABLE}"
    RMDir /r "$INSTDIR\cache"
    RMDir /r "$INSTDIR\logs"
    MessageBox MB_YESNO|MB_ICONQUESTION|MB_DEFBUTTON2 "Also delete ${INFO_PRODUCTNAME} data (domains, credentials, login)?$\r$\n$\r$\nChoose No to keep it for a later reinstall." /SD IDNO IDNO keepData
        RMDir /r "$INSTDIR\data"
    keepData:

    # WebView2 data path used by releases before the CZL layout
    RMDir /r "$APPDATA\${PRODUCT_EXECUTABLE}"

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    Delete "$INSTDIR\uninstall.exe"
    DeleteRegKey HKCU "${UNINST_KEY}"

    # Non-recursive: removed only when empty, so a kept data\ keeps its parents
    RMDir "$INSTDIR"
    RMDir "$LOCALAPPDATA\CZL"
SectionEnd
