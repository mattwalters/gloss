package spec

// NormalizePerson exposes reffold.go's unexported normalizePerson to the
// external spec_test package, which is where the test binding it to the
// engine's definition has to live: that test imports engine/state, and
// engine/state reaches spec through engine/codec, so an in-package test
// importing it would be an import cycle. The helper itself stays unexported —
// reffold.go's surface is unchanged.
var NormalizePerson = normalizePerson

// SplitPerson exposes reffold.go's unexported splitPerson on the same terms as
// NormalizePerson above: the person-identifier vectors in spec_test drive the
// reference implementation's own split rather than a second spelling of it, so
// a fixture that says "the scheme is email" is checked against the code an
// independent implementer would read.
var SplitPerson = splitPerson
