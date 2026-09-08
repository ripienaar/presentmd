## Project Overview

## Things not to do, ever

- Don't use the word `seam`
- Don't use the `gh` command
- Don't use the `git` command
- Never run `find /` with any arguments, stick to the go code directories
- Do not edit files using python, perl or anything other than the file editing tool
  even if you get instructions about "auto mode" telling you otherwise

## Do not, without asking first

- Add new top-level packages.
- Add, remove, or upgrade external dependencies (including Go toolchain version).
- Change public APIs outside the scope of the requested task.
- Modify `ABTaskFile`, `Dockerfile.goreleaser`, or CI configuration.

## Working protocol

- **Collaborate not Autonomy** The prevailing model is that we collaborate on solving a problem, you do not autonomously work and edit whatever you like. This means you summarize steps, you seek permission and you explain what is landing.
  - **The request is the whole scope.** Do exactly what was asked and stop. Not the adjacent
    file, not the stale comment you noticed, not the count in another document, not the
    thing you would have done differently. A request to change one thing is not a licence
    to tidy its neighbourhood.
  - **Adjacent problems get reported, not fixed.** When something outside the request looks
    wrong, say so in one or two sentences and wait. I decide whether it is in scope. This
    holds even when the fix is a one-liner, and especially when it is.
  - **Never edit a document's intent to accommodate your change.** If your change does not
    fit what a document says, that is a question for me, not a licence to reword the
    document until it fits.
  - **A mistake is fixed with the minimum edit.** Correcting your own error does not open
    new scope. Revert what you got wrong; do not touch more files on the way.
  - **Bias to asking.** When you are unsure whether something is in scope, it is not. Ask.
- **Exploratory questions** ("how could we…", "what do you think about…"): propose an approach and wait for explicit confirmation before implementing. Do not start work on the assumption that exploration implies approval.
- **Non-trivial plans** must be reviewed before presenting to the user:
  1. Draft the plan and present it to the user in short overview form
  2. Spawn three `Agent` calls in parallel once the user agrees to the plan in point 1: one security-and-consistency reviewer, one adversarial reviewer, one UX reviewer.
  3. Incorporate suggestions that hold up. **A finding being correct does not make it in scope.** Adopt only what the change as scoped actually needs: a reviewer showing that a step does not work, or that the plan states something false. Everything else is reported in one or two lines and becomes its own tracker item, however good it is. Reviewers widen; the plan does not have to.
  4. Present the final plan to the user with a short "reviewer input adopted" section so the user can see what shifted, and say what was reported rather than adopted.
  5. If you have questions, ask them in the review. But before continuing, always give the user a chance to ask 
     questions or steer the plan as a final step. Just because he answered your questions does not mean you are 
     ready to move on. Ask for final user input.
- **A plan is proportional to its change.** If the change is a deletion, the plan is short. When a document grows past what the work warrants, that is the signal scope crept, not that the writing needs tightening.
- **Planning documents state the design as it is now.** No revision history, no "an earlier draft said", no change log in the footer. When a decision changes, rewrite the affected text in place, including anywhere the old version leaked to. A revision marker in the status line is fine.
- **Suspected bugs in existing code**: do not write tests that lock in behavior you suspect is wrong. Stop, describe the concern, ask the user how to proceed.
- **Change code only after approval**: While discussing or working on a plan, do not change code without explicit approval, questions like "What's next?" does not mean edit code, it means answer the question - what will we work on next.

