package pkg

const (
	substitutionPackageName    = "${{package.name}}"
	substitutionPackageVersion = "${{package.version}}"
	substitutionPackageEpoch   = "${{package.epoch}}"
	substitutionTargetsDestdir = "${{targets.destdir}}"
	substitutionSubPkgDir      = "${{targets.subpkgdir}}"
)

func substitutionReplacements() map[string]string {
	return map[string]string{
		substitutionPackageName:    "MELANGE_TEMP_REPLACEMENT_PACAKAGE_NAME",
		substitutionPackageVersion: "MELANGE_TEMP_REPLACEMENT_PACAKAGE_VERSION",
		substitutionPackageEpoch:   "MELANGE_TEMP_REPLACEMENT_PACAKAGE_EPOCH",
		substitutionTargetsDestdir: "MELANGE_TEMP_REPLACEMENT_DESTDIR",
		substitutionSubPkgDir:      "MELANGE_TEMP_REPLACEMENT_SUBPKGDIR",
	}
}
