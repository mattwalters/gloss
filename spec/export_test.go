package spec

// NormalizePerson exposes reffold.go's unexported normalizePerson to the
// external spec_test package, which is where the test binding it to the
// engine's definition has to live: that test imports engine/state, and
// engine/state reaches spec through engine/codec, so an in-package test
// importing it would be an import cycle. The helper itself stays unexported —
// reffold.go's surface is unchanged.
var NormalizePerson = normalizePerson
