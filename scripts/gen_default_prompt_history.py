#!/usr/bin/env python3
"""Regenerates default_prompt_history.go from the git history of default_prompts/.

Run it from the repository root after changing a default prompt, before
committing, so installations that never customised the old version get the
new one on their next start.
"""
import glob, hashlib, os, subprocess, sys


def git(*args, binary=False):
    r = subprocess.run(['git', *args], capture_output=True, text=not binary)
    if r.returncode != 0:
        sys.exit(f"git {' '.join(args)} failed: {r.stderr if not binary else r.stderr.decode(errors='replace')}")
    return r.stdout


# A shallow clone lacks the older versions; the list would silently miss them.
if git('rev-parse', '--is-shallow-repository').strip() == 'true':
    sys.exit('shallow clone: run `git fetch --unshallow` first, or the history is incomplete')

out = ['// Code generated from the git history of default_prompts/; do not edit by hand.',
       '// Regenerate with scripts/gen_default_prompt_history.py after changing a default prompt.',
       '', 'package main', '',
       '// previousDefaultPrompts holds the SHA-256 of every earlier version of each',
       '// default prompt. A prompt in prompts/ that still matches one of them was',
       '// never customised, so loadTemplates replaces it with the current default.',
       'var previousDefaultPrompts = map[string][]string{']
for f in sorted(glob.glob('default_prompts/*.tmpl')):
    current = hashlib.sha256(open(f, 'rb').read()).hexdigest()
    shas = set()
    for c in git('log', '--format=%H', '--', f).split():
        # A commit that deleted the file has no version of it to record.
        if subprocess.run(['git', 'cat-file', '-e', f'{c}:{f}'], capture_output=True).returncode != 0:
            continue
        shas.add(hashlib.sha256(git('show', f'{c}:{f}', binary=True)).hexdigest())
    shas.discard(current)
    if shas:
        out.append(f'\t"{os.path.basename(f)}": {{')
        out += [f'\t\t"{s}",' for s in sorted(shas)]
        out.append('\t},')
out.append('}')
open('default_prompt_history.go', 'w').write('\n'.join(out) + '\n')
