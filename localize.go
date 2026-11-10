package main

import (
	"strings"

	"github.com/thedavidweng/any2jb/locales"
)

// dynamicSegments maps user-facing sentences produced by the readiness,
// session, driver, retained, diagnostics and manifest adapters onto catalog
// messages, so model text follows the app language. Entries apply
// longest-first because adapters compose sentences with live data; unknown
// text passes through unchanged.
var dynamicSegments = []struct {
	source string
	text   func() string
}{
	{"Unavailable: OunceCake's GPL v2-family obligations (exact grant, corresponding source, modifications and notices) are unresolved for any2jb. A developer may stage these pinned sources locally; no package includes them.", locales.DynDistOunceCake},
	{"Unavailable: the SD browser's GPL-2.0-only obligations and inherited module rights are unresolved for any2jb. A developer may stage these pinned sources locally; no package includes them.", locales.DynDistSDBrowser},
	{"Unavailable: MintBrew has no blanket redistribution grant (GPUBrewLauncher lineage) and its rights review is unresolved. A developer may stage these pinned sources locally; no package includes them.", locales.DynDistMintbrew},
	{"Unavailable: the MIT native HBLauncher fork still stages MintBrew sources whose rights review is unresolved. A developer may stage these pinned sources locally; no package includes them.", locales.DynDistMintbrewNative},
	{"Unavailable: the IZlauncher's GPL-2.0-or-later scope and inherited rights are unresolved for any2jb. A developer may stage these pinned sources locally; no package includes them.", locales.DynDistIZLauncher},
	{"Unavailable: the BlitBun module's rights review is unresolved (IZ2S-Platform GPL-2.0-or-later scope). A developer may stage these pinned sources locally; no package includes them.", locales.DynDistIZBlitbun},
	{"Unavailable: the IZlauncher shell module's rights review is unresolved (IZ2S-Platform GPL-2.0-or-later scope). A developer may stage these pinned sources locally; no package includes them.", locales.DynDistIZShell},
	{"Unavailable: the VacooTube module's rights review is unresolved (IZ2S-Platform GPL-2.0-or-later scope). A developer may stage these pinned sources locally; no package includes them.", locales.DynDistIZVacootube},
	{"Unavailable: the portable homebrew runtime bundles third-party interpreters and libraries whose redistribution notices are unreviewed. A developer may stage these pinned sources locally; no package includes them.", locales.DynDistHomebrewRuntime},
	{"Unavailable: launcher save redistribution and bundled third-party data review are unresolved. No launcher save is bundled.", locales.DynDistLauncherArchive},
	{"Unavailable: Artemis has no upstream license at the pinned revision. No launcher save is bundled.", locales.DynDistLauncherArtemis},
	{"Trigger-installer distribution is blocked pending review of its derived save template and vendored dependencies. No installer, Python interpreter or save template is bundled.", locales.DynDistTriggerInstaller},
	{"USB driver distribution is blocked pending review of exact binary source/build materials and MIT/APSL notice obligations. No driver package or installer is bundled.", locales.DynDistUSBDriver},
	{"MIT permits redistribution with its copyright and permission notice. Source record only; compiled helpers and dependencies need their own records.", locales.DynDistMITSourceOnly},
	{"MIT permits redistribution with its copyright and permission notice. Source record only; it runs on the console through a developer-staged loader.", locales.DynDistMITLoader},
	{"Unavailable: the research probe set has no redistribution grant for any2jb. Permission reported for mac2jb does not authorize this package.", locales.DynDistBrowserResearch},
	{"More than one controller was discovered. Verify the connected controller before using USB input, or choose guided manual input.", locales.DynUsbVerifyController},
	{"Control Center distribution is blocked pending review of the complete separately authored release inventory and its notices.", locales.DynDistControlCenter},
	{"Connect a USB-C data cable so the Mac receives power from the console; verify keyboard enumeration before sending input.", locales.DynUsbConnectCable},
	{"Original any2jb code. It only drives separately staged pinned read-only tool sources and contains no console write path.", locales.DynDistOriginalTools},
	{"Connect the console by Ethernet on the reachable LAN; host listening and console connection are checked during launch.", locales.DynNetConnectConsole},
	{"USB input requires permitted, integrity-verified helper binaries. Guided manual input does not require these helpers.", locales.DynRtHelpersRequired},
	{"Check driver loading and the USB-C data cable direction, then refresh readiness; or choose guided manual input.", locales.DynUsbCheckCableDirection},
	{"Complete macOS driver approval, restart if requested, then refresh readiness; or choose guided manual input.", locales.DynUsbCompleteApproval},
	{"One attached USB gadget controller was discovered. Keyboard enumeration and console input are not verified.", locales.DynUsbOneController},
	{"Verify keyboard enumeration on the connected console before using USB input, or choose guided manual input.", locales.DynUsbVerifyEnumeration},
	{"Official Node binary and complete license notice; archive verified against release SHASUMS256.txt.", locales.DynDistNode},
	{"Multiple attached USB gadget controllers were discovered; the connected controller is ambiguous.", locales.DynUsbMultipleControllers},
	{"CivSwitch payload distribution is blocked pending derived-payload and dependency notice review.", locales.DynDistCivPayload},
	{"Choose guided manual input. Ordinary USB-C ports do not establish USB device-mode support.", locales.DynUsbChooseManual},
	{"Inherited ns2jb redistribution permission is unresolved. No host or bootstrap is bundled.", locales.DynDistNs2jb},
	{"The running kernel reports the USB gadget kext loaded. USB enumeration is not verified.", locales.DynUsbKextLoaded},
	{"Check driver inspection access, then refresh readiness; or choose guided manual input.", locales.DynUsbCheckInspection},
	{"The USB gadget kext bundle is present. Presence does not prove integrity or loading.", locales.DynUsbKextPresent},
	{"Review driver setup and macOS approval requirements, or choose guided manual input.", locales.DynUsbReviewDriver},
	{"Check USB registry access and refresh readiness, or choose guided manual input.", locales.DynUsbCheckRegistry},
	{"Check the driver restart marker and system boot time, then refresh readiness.", locales.DynUsbCheckRestartMarker},
	{"The USB gadget kext is treated as not loaded until restart state is clear.", locales.DynUsbKextTreatedUnloaded},
	{" Developer staging is required; no payload or composite helper is bundled.", locales.DynCivStagingRequiredSuffix},
	{"Built-in USB device-mode input is not supported by this platform adapter.", locales.DynUsbUnsupported},
	{"driver installation did not complete; refresh readiness before retrying: ", locales.DynDrvInstallIncompletePrefix},
	{" A local developer stage was attempted and failed its integrity check.", locales.DynToolStageFailedSuffix},
	{"Choose an assigned LAN IPv4 address below, then refresh readiness.", locales.DynNetChooseAddress},
	{". Local assignment verified; console connectivity is not verified.", locales.DynNetSelectedSuffix},
	{"Install an intact app bundle with its matching runtime resources.", locales.DynRtInstallIntact},
	{"Restart this Mac, then refresh readiness before using USB input.", locales.DynUsbRestartAction},
	{"MIT template asset; retain MyGo copyright and permission notice.", locales.DynDistMITTemplate},
	{"CivSwitch components are not staged; this path is unavailable.", locales.DynCivNotStaged},
	{"Connect this Mac to the console LAN, then refresh readiness.", locales.DynNetConnectLAN},
	{"Required notice for the MyGo framework and placeholder icon.", locales.DynDistMygoNotice},
	{"USB helper integrity verified; enumeration is not verified.", locales.DynRtHelpersVerified},
	{": attached gadget controller, available but not verified.", locales.DynUsbControllerSuffix},
	{"This component is not recorded in the canonical manifest.", locales.DynToolNotInManifest},
	{"administrator authorization or driver installation failed", locales.DynDrvAdminFailed},
	{"Original any2jb code; no upstream engine or GUI scripts.", locales.DynDistOriginal},
	{"Check network interface access, then refresh readiness.", locales.DynNetCheckAccess},
	{" (available but not verified for console connectivity)", locales.DynNetRecommendedSuffix},
	{"This read-only tool is not implemented in this build.", locales.DynToolNotImplemented},
	{"CivSwitch composite HID requires Apple Silicon macOS", locales.DynCivRequiresAppleSilicon},
	{"No attached USB gadget controllers were discovered.", locales.DynUsbNoControllers},
	{"The bundled tool host is unavailable in this build.", locales.DynToolHostUnavailable},
	{"The USB gadget kext bundle could not be inspected.", locales.DynUsbKextNotInspected},
	{"Choose an ns2jb host with an implemented verifier.", locales.DynRtChooseVerifier},
	{"USB controller discovery could not be completed.", locales.DynUsbDiscoveryFailed},
	{"This engine cannot launch from the packaged app.", locales.DynRtEnginePackaged},
	{"CivSwitch developer root is not a real directory", locales.DynCivRootNotDirectory},
	{"driver installation requires Apple Silicon macOS", locales.DynDrvRequiresAppleSilicon},
	{"LAN interface discovery could not be completed.", locales.DynNetDiscoveryFailed},
	{"Simulated readiness, not a real hardware check.", locales.DynFakeSimulatedReadiness},
	{"Driver restart state could not be determined.", locales.DynUsbRestartUnknown},
	{"A driver installation requires a Mac restart.", locales.DynUsbRestartRequired},
	{"Developer-staged engine, not redistributable.", locales.DynRtDeveloperStaged},
	{"Loaded driver state could not be inspected.", locales.DynUsbLoadedUnknown},
	{"No driver installation restart is pending.", locales.DynUsbNoRestartPending},
	{"driver source root is not a real directory", locales.DynDrvRootNotDirectory},
	{"CivSwitch developer root must be absolute", locales.DynCivRootAbsolute},
	{"Bundled Node runtime integrity verified.", locales.DynRtNodeVerified},
	{"Verified USB helpers are not available.", locales.DynRtHelpersUnavailable},
	{"No usable LAN IPv4 address was found.", locales.DynNetNoUsableLAN},
	{"The USB gadget kext bundle is absent.", locales.DynUsbKextAbsent},
	{" No unambiguous address is selected.", locales.DynNetNoSelection},
	{"driver source root must be absolute", locales.DynDrvRootAbsolute},
	{"The USB gadget kext is not loaded.", locales.DynUsbKextNotLoaded},
	{"CivSwitch manifest is unavailable", locales.DynCivManifestUnavailable},
	{"session owner cannot be proven", locales.DynRecOwnerUnproven},
	{"Assigned LAN IPv4 addresses: ", locales.DynNetAssignedPrefix},
	{"unexpected driver bundle file", locales.DynDrvUnexpectedFile},
	{"session owner is still alive", locales.DynRecOwnerAlive},
	{"Runtime staging failed.", locales.DynRtStagingFailed},
	{"CivSwitch unavailable: ", locales.DynCivUnavailablePrefix},
	{"missing driver manifest", locales.DynDrvMissingManifest},
	{" Recommended address: ", locales.DynNetRecommendedPrefix},
	{"invalid session lease", locales.DynRecInvalidLease},
	{" Selected: ", locales.DynNetSelectedPrefix},
}

func localize(text string) string {
	for _, segment := range dynamicSegments {
		if strings.Contains(text, segment.source) {
			text = strings.ReplaceAll(text, segment.source, segment.text())
		}
	}
	return text
}
