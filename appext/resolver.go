package appext

// PackageResolver resolves Android UID to package name.
// Implemented by native Android layer (Java/Kotlin) calling
// context.getPackageManager().getPackagesForUid(uid).
//
// gomobile exports this as AppextPackageResolver (ObjC) / appext.PackageResolver (Java).
type PackageResolver interface {
	PackageForUID(uid int32) string
}
