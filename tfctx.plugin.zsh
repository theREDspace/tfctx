# Entry point for oh-my-zsh and other zsh plugin managers. The logic lives in
# the tfctx binary; this only loads its zsh integration.
if (( $+commands[tfctx] )); then
  eval "$(command tfctx init zsh)"
else
  print -u2 "tfctx: the tfctx binary isn't on your PATH. See https://github.com/theREDspace/tfctx#installation"
fi
