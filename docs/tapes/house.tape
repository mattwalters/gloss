# Sourced first by every tape in this directory (never rendered itself — see
# the Makefile's render-tape recipe), so terminal geometry, timing and
# typing speed have one spelling instead of one per cast.
#
# Require reads the environment the render-tape recipe invokes vhs with, not
# the recorded shell's — it runs as a preflight before a single keystroke is
# typed. The recipe puts the writ binary on PATH before it calls vhs.
Require writ

Set Shell "bash"
Set FontSize 14
Set Width 1200
Set Height 640
Set Padding 16
Set Framerate 16
Set TypingSpeed 45ms
Set WaitTimeout 20s
Set Theme "catppuccin-mocha"
