# lint: groovy source

# Invoke the linting tool
% make lint-groovy

# Alt checking: groovy interpreter

Odd syntax errors can be detected by the groovy interpreter.

% groovy path/to/{source}.groovy

jjb/ pipeline scripts will eventually due to syntax problems but groovy can
still detect problems like mismatched quotes, invalid op tokens, etc.
