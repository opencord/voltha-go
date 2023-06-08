# -*- makefile -*-
# -----------------------------------------------------------------------
# Intent: Dependency-free macros used to source library makefiles
#         and define the build environment.
# -----------------------------------------------------------------------

$(if $(DEBUG-bootstrap_mk),$(warning ENTER))

##---------------------##
##---]  CONSTANTS  [---##
##---------------------##
is-false = $(if $(1),true,$(null))
is-true  = $(if $(1),$(null),true)

##--------------------------##
##---]  LIBRARY MACROS  [---##
##--------------------------##
is-null = $(if $(1),$(null),$(error $(1)= is undef))
# is-null-var = $(if $($(1)),$(null),$(error $(1)= is undef))

## [origin]
## foreach: var=automatic, origin=undef
is-null-var-orig = $(strip \
  $(foreach varname,$(1),\
      $(warning ** is-null-var: varname=$(varname), origin=$(origin 1), origin=$(origin var))\
  )\
)
#  $(if $(1),$(call is-null-var-indirect,$($(1))),$(error $(1)= is undef)))\

is-null-var-indirect = $(if $(1),$(null),$(error $(1)= is undef))

# origin - undefned
# default
# environment
# environment override
# automatic
# null(blah) ?   - true

## -----------------------------------------------------------------------
## Intent: Given an indirect var containing varname of a makefile *_ROOT
##         parent director, derive a *_MKDIR variable and conditionally
##         include the makefile hierarchy.
## -----------------------------------------------------------------------
## Given:
##   o var containing OPT_ROOT=path
## Return:
##   o OPT_MKDIR=$(OPT_ROOT)/makefiles
##   o If exists include $(OPT_MKDIR)/include.mk
## -----------------------------------------------------------------------
# library-include   := $(call mk-library-include,blah)

mk-library-include=$(strip \
  $(warning mk-library-include: $$1[$(1)] = [$($(1))]))\
  $(call is-null-var,1)\
  $(foreach var,$($(1)),\
    $(info var=$(var) is-null=$(call is-null-var,var))\
  $(foreach val,$$(var),\
    $(info val=$(val))\
    $(foreach makedir,$(subst _ROOT,_MKDIR,$(var)),\
$(warning makedir=$(makedir))\
)\
)\
)\
)


# $(warning makedir=$(makedir))\
      $(if $($(makedir)),$(null),\
        $(eval $(makedir)=$$$$($(var))/makefiles)\
$(warning $(makedir) = $($($(makedir))))\
$(info $$(wildcard $(val)/makefiles/include.mk) = $(wildcard $(val)/makefiles/include.mk))\
        $(foreach mf,$(wildcard $(wildcard $(val)/makefiles/include.mk)),\
$(warning $$(eval include $(mf)))\
          $(eval include $(mf)))\
))))\
)

$(if $(DEBUG-bootstrap_mk),$(warning LEAVE))

# [EOF]
