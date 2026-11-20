---
type: Reference
title: Rubric-graded model judges
description: How rubric-based model judges are built, calibrated against human labels and kept reliable, what Microsoft's Copilot Studio kit provides and lacks, and which open-source tools cover the gaps, as researched on 2026-11-20 for a future model-judge grader.
resource: https://learn.microsoft.com/en-us/microsoft-copilot-studio/guidance/kit-rubrics-overview
tags: [references, evals, model-judge, rubrics]
status: draft
stale_after: 2027-05-20T00:00:00Z
sources:
  - id: ms-overview
    resource: https://learn.microsoft.com/en-us/microsoft-copilot-studio/guidance/kit-rubrics-overview
    title: Copilot Studio kit, rubrics overview
  - id: ms-workflow
    resource: https://learn.microsoft.com/en-us/microsoft-copilot-studio/guidance/kit-rubrics-refinement-workflow
    title: Copilot Studio kit, rubric refinement workflow
  - id: ms-best
    resource: https://learn.microsoft.com/en-us/microsoft-copilot-studio/guidance/kit-rubrics-best-practices
    title: Copilot Studio kit, best practices for rubric refinement
  - id: ms-reference
    resource: https://learn.microsoft.com/en-us/microsoft-copilot-studio/guidance/kit-rubrics-reference
    title: Copilot Studio kit, rubrics reference
  - id: ms-tests
    resource: https://learn.microsoft.com/en-us/microsoft-copilot-studio/guidance/kit-rubrics-tests
    title: Copilot Studio kit, use rubrics in tests
  - id: ms-run
    resource: https://learn.microsoft.com/en-us/microsoft-copilot-studio/guidance/kit-run-tests
    title: Copilot Studio kit, run tests
  - id: anthropic-evals
    resource: https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents
    title: "Anthropic: Demystifying evals for AI agents"
  - id: anthropic-stats
    resource: https://www.anthropic.com/research/statistical-approach-to-model-evals
    title: "Anthropic: A statistical approach to model evaluations (Miller, arXiv 2411.00640)"
  - id: claude-tests
    resource: https://platform.claude.com/docs/en/test-and-evaluate/develop-tests
    title: "Claude docs: Define success and develop tests"
  - id: openai-bp
    resource: https://developers.openai.com/api/docs/guides/evaluation-best-practices
    title: "OpenAI: Evaluation best practices"
  - id: openai-graders
    resource: https://developers.openai.com/api/docs/guides/graders
    title: "OpenAI: Graders"
  - id: mt-bench
    resource: https://arxiv.org/abs/2306.05685
    title: Zheng et al., Judging LLM-as-a-Judge with MT-Bench and Chatbot Arena
  - id: tau-bench
    resource: https://arxiv.org/abs/2406.12045
    title: tau-bench (pass^k)
  - id: position
    resource: https://arxiv.org/abs/2305.17926
    title: Wang et al., Large Language Models are not Fair Evaluators
  - id: length
    resource: https://arxiv.org/abs/2404.04475
    title: Dubois et al., Length-Controlled AlpacaEval
  - id: self-pref
    resource: https://arxiv.org/abs/2404.13076
    title: Panickssery et al., LLM evaluators recognize and favor their own generations
  - id: panel
    resource: https://arxiv.org/abs/2404.18796
    title: Verga et al., Replacing judges with juries (panel of LLM evaluators)
  - id: mchugh
    resource: https://pmc.ncbi.nlm.nih.gov/articles/PMC3900052/
    title: McHugh, Interrater reliability, the kappa statistic
  - id: inspect-metrics
    resource: https://raw.githubusercontent.com/UKGovernmentBEIS/inspect_ai/main/docs/metrics.qmd
    title: Inspect AI, metrics (pass_k, bootstrap_stderr, krippendorff_alpha)
  - id: inspect-options
    resource: https://inspect.aisi.org.uk/options.html
    title: Inspect AI, options (epochs and reducers)
  - id: inspect-scorers
    resource: https://inspect.aisi.org.uk/scorers.html
    title: Inspect AI, scorers (model_graded_qa, multi_scorer)
  - id: sklearn-kappa
    resource: https://scikit-learn.org/stable/modules/generated/sklearn.metrics.cohen_kappa_score.html
    title: scikit-learn cohen_kappa_score
  - id: docker-agent
    resource: /references/docker-agent.md
    title: Docker Agent reference (docker agent eval)
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-20T23:00:00Z }
---

# What a rubric is

- A rubric states what a good answer looks like, a grade scale with a definition per grade, and
  good and bad examples with the reason for each. Microsoft's Copilot Studio kit uses a fixed 1 to
  5 scale and 3 to 5 observable, domain-specific dimensions; more than about 10 criteria are too
  many for people or a model to apply consistently.
- Graders are code, model or human; the guidance is to choose deterministic graders where possible
  and model graders only where necessary, and to calibrate model graders against human ones.
  Format, schema, required elements, citations being present, forbidden terms, scope and the end
  state are code checks; tone, coherence and whether a claim is supported by its source need a
  model judge.
- Binary pass/fail or pairwise comparison is more reliable than a fine scale; the judge reasons
  before its verdict and has an "Unknown" answer.

Sources: [Copilot Studio kit, best practices](https://learn.microsoft.com/en-us/microsoft-copilot-studio/guidance/kit-rubrics-best-practices); [Anthropic: Demystifying evals for AI agents](https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents); [OpenAI: Evaluation best practices](https://developers.openai.com/api/docs/guides/evaluation-best-practices); [Claude docs: Define success and develop tests](https://platform.claude.com/docs/en/test-and-evaluate/develop-tests).

# Microsoft's kit: what it provides

- Two modes: testing (a grade only; a case passes at the passing grade, 5 by default) and
  refinement (a grade with the judge's reasoning; the passing grade is informational).
- An iterative loop: run, review, grade blind (the AI's grades hidden, since seeing them biases
  people), refine (an AI rewrites the rubric from the human and AI grades and the marked examples),
  save as a new copy, re-run the same test set ("Duplicate Run"), compare alignment, repeat. Stop
  at about 75 to 90 % alignment or when an iteration gains about 1 %; after 4 to 5 iterations test
  on fresh cases (30 to 50 tuning cases, 10 to 20 held out) to avoid overfitting; two or three
  people grade critical rubrics independently; re-enter refinement periodically, as rubrics drift.
- Alignment is the share of cases where the AI's grade equals the human's, or an average of
  `1 - |AI - human| / 4` per case (the pages give both and do not say which the kit shows), with
  counts of too-lenient and too-strict grades.

Sources: [overview](https://learn.microsoft.com/en-us/microsoft-copilot-studio/guidance/kit-rubrics-overview); [refinement workflow](https://learn.microsoft.com/en-us/microsoft-copilot-studio/guidance/kit-rubrics-refinement-workflow); [reference](https://learn.microsoft.com/en-us/microsoft-copilot-studio/guidance/kit-rubrics-reference); [use rubrics in tests](https://learn.microsoft.com/en-us/microsoft-copilot-studio/guidance/kit-rubrics-tests); [run tests](https://learn.microsoft.com/en-us/microsoft-copilot-studio/guidance/kit-run-tests).

# Microsoft's kit: what it lacks

- No measure of how much the judge's grade varies when the same answer is graded again; no
  aggregation over repeated runs or comparison view between runs.
- No version history ("no version history in MVP"); versions are copies named v1, v2; nothing says
  a result records which rubric version graded it. Governance is planned.
- No judge-model choice, no control of the judge's own biases, and no chance-corrected agreement
  (kappa, alpha); human-human agreement is advised but not computed.

Sources: [overview](https://learn.microsoft.com/en-us/microsoft-copilot-studio/guidance/kit-rubrics-overview); [refinement workflow](https://learn.microsoft.com/en-us/microsoft-copilot-studio/guidance/kit-rubrics-refinement-workflow).

# Covering the gaps

- **Judge variance**: grade the same answer several times and report the mean, its standard
  error and, where items share a source, a clustered error; compare two runs by paired
  differences. No source fixes the number of samples.
- **Repeated trials**: pass^k is the probability that all k trials succeed (tau-bench; gpt-4o-level
  agents were below 25 % at pass^8 in its retail tasks).
- **Agreement**: weighted kappa for ordinal scales; Krippendorff's alpha for several raters,
  missing ratings and ordinal data, with a bootstrap interval over units. McHugh calls a kappa
  below 0.60 inadequate and asks for percent agreement beside it. The alpha thresholds 0.800 (rely
  on) and 0.667 (tentative) are confirmed only by secondary sources here; Gwet's AC1/AC2 address
  kappa's low values when one label dominates (unverified).
- **Judge biases**: position, verbosity and self-enhancement; strong judges agree with people over
  80 % of the time, about as often as people agree with each other. Position bias is reduced by
  scoring both orders (pairwise only); length bias by a length-controlled comparison (AlpacaEval's
  correlation with Chatbot Arena rose from 0.94 to 0.98); self-preference grows with a judge's
  ability to recognise its own text, so the judge is from another model than the generator; a
  panel of smaller judges from several families showed less self-preference at about a seventh of
  the cost.
- **Pinning**: graders take dated model snapshots, a seed and a temperature.

Sources: [Anthropic: A statistical approach to model evaluations](https://www.anthropic.com/research/statistical-approach-to-model-evals); [tau-bench](https://arxiv.org/abs/2406.12045); [McHugh](https://pmc.ncbi.nlm.nih.gov/articles/PMC3900052/); [MT-Bench](https://arxiv.org/abs/2306.05685); [Wang et al.](https://arxiv.org/abs/2305.17926); [Length-Controlled AlpacaEval](https://arxiv.org/abs/2404.04475); [Panickssery et al.](https://arxiv.org/abs/2404.13076); [Verga et al.](https://arxiv.org/abs/2404.18796); [OpenAI: Graders](https://developers.openai.com/api/docs/guides/graders).

# Open-source tools (2026-11-20)

| Tool | Licence | Covers |
|------|---------|--------|
| Docker Agent `docker agent eval` | Apache-2.0 | recorded sessions scored by assertions, tool-call F1 and an LLM or evaluator judge; the first candidate (Principle VI), not yet assessed for the gaps |
| Inspect AI (UK AISI), 0.3.291 | MIT | rubric model scorers, judge panels (`multi_scorer`), epochs with `pass_k_{k}` ("probability that all k epoch attempts succeed"), `stderr` and `bootstrap_stderr`, `krippendorff_alpha` (nominal, ordinal, interval); OpenSSF Scorecard not scanned |
| promptfoo, DeepEval, Ragas | promptfoo unconfirmed; Apache-2.0 | rubric scorers and repeats; no agreement metrics |
| scikit-learn | BSD | `cohen_kappa_score` with linear or quadratic weights (Scorecard 9.4) |
| `krippendorff`, irrCAC | GPL | alpha; Gwet AC1/AC2 (Python irrCAC has one maintainer) |
| OpenAI Evals | MIT | last release 2024-05: stale |

No tool records the rubric's version and the judge model with each result, or computes
judge-to-human agreement against a labelled set by itself.

Sources: [Inspect AI metrics](https://raw.githubusercontent.com/UKGovernmentBEIS/inspect_ai/main/docs/metrics.qmd); [Inspect AI options](https://inspect.aisi.org.uk/options.html); [Inspect AI scorers](https://inspect.aisi.org.uk/scorers.html); [scikit-learn cohen_kappa_score](https://scikit-learn.org/stable/modules/generated/sklearn.metrics.cohen_kappa_score.html); [Docker Agent reference](/references/docker-agent.md).
