#!/bin/bash
# Regenerate module diagrams with codex image generation.
#
#   diagrams/generate.sh 10-padding 15-traversal   # regenerate some
#   diagrams/generate.sh                           # regenerate all
#
# Each prompt in prompts.json is wrapped with the shared hand-drawn style
# (style_prefix/style_suffix) and the result is written to images/<id>.webp.
# Review every image before committing: image models misspell labels.
set -euo pipefail
cd "$(dirname "$0")/.."
ids=("$@")
if [ ${#ids[@]} -eq 0 ]; then
  mapfile -t ids < <(python3 -c "import json;[print(x['id']) for x in json.load(open('diagrams/prompts.json'))['diagrams']]")
fi
gen() {
  id=$1
  prompt=$(python3 -c "import json,sys;d=json.load(open('diagrams/prompts.json'));x=[x for x in d['diagrams'] if x['id']==sys.argv[1]][0];print(d['style_prefix']+x['prompt']+d['style_suffix'])" "$id")
  tmp=$(mktemp -d)
  (cd "$tmp" && codex exec --skip-git-repo-check --sandbox workspace-write \
    "Use your built-in image generation tool to create exactly one image from the prompt below and save it as $id.png in the current directory. Do not do anything else. Prompt: $prompt" >/dev/null 2>&1)
  cwebp -quiet -q 85 -resize 1600 0 "$tmp/$id.png" -o "images/$id.webp"
  rm -rf "$tmp"
  echo "images/$id.webp"
}
export -f gen
printf '%s\n' "${ids[@]}" | xargs -P 5 -I{} bash -c 'gen {}'
