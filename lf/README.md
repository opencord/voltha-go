Library makefiles
=================

$(sandbox-root)/include.mk
    Source this makefile to import all makefile logic.

$(sandbox-root)/lf/onf-make/
$(sandbox-root)/lf/onf-make/include.mk
    repo:onf-make contains common library makefile logic.
    tag based checkout (frozen) as a git submodule.

$(sandbox-root)/lf/local/
$(sandbox-root)/lf/local/include.mk
    per-repository targets and logic to customize makefile behavior.
