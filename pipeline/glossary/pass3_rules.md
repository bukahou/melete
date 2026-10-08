# Glossary PASS 3 — write glossary entries

You write glossary entries for a certification-exam study app (Melete).

## Input
`pass3_in/<batch>.json` → `{"bank", "terms": [{"slug", "category", "example"}]}`
- `slug`: the canonical term (AWS: official English name; IPA: Japanese standard term). **Do not change it.**
- `category`: the group it belongs to (AWS service / IPA 中分類). Context only.
- `example`: the opening of one exam question where the term appears. **Use it only to pick the right meaning of an ambiguous term** (e.g. IPA "PLC" next to BLE/LPWA = 電力線通信, not a programmable controller). Never mention the question in the entry.

## Output
`pass3_out/<same batch name>.json` →
```json
{"bank": "<same>", "terms": [
  {"slug": "<same as input>",
   "names": {"zh": "...", "ja": "..."},
   "reading": "<IPA only>",
   "definition": {"zh": "...", "ja": "..."}}
]}
```
One entry for EVERY input term, same order, same slug.

## AWS banks (aws-saa-c03, aws-sap-c02)
- `names.zh`: the name Chinese AWS documentation uses. If Chinese docs keep the English product name, keep it (optionally add a short Chinese gloss in parentheses, e.g. "Amazon S3 Transfer Acceleration（S3 传输加速）"). Concepts get their standard Chinese term ("最小权限").
- `names.ja`: the name Japanese AWS documentation uses (e.g. マルチパートアップロード, プライベートホストゾーン, リザーブドインスタンス). Product names stay in English when the Japanese docs keep them.
- No `reading`.
- `definition.zh` ≤ 80 Chinese characters, `definition.ja` ≤ 100 Japanese characters, 1–2 sentences each.

## IPA bank (ipa-ip)
- `names`: **only `ja`** (= the slug, or its standard full form). **No `zh` key at all** — this bank is Japanese-only by product decision.
- `reading`: hiragana reading of the term (e.g. ちょさくけん, えーびーしーぶんせき; alphabet abbreviations are read letter by letter in hiragana: PKI → ぴーけーあい).
- `definition`: **only `ja`**, ≤ 100 Japanese characters, IPA-textbook style (use「，」「。」like IPA materials is fine).

## Definition quality (all banks)
- A general, self-contained definition: what it is + what it is for / its key property. Exam-level accuracy.
- **Never** mention any question, scenario, company, or answer choice. **Never** copy sentences from the example.
- Never invent product names, limits, or numbers you are not sure of. If a detail is uncertain, leave it out.
- Prefer the property the exam tests (e.g. Spot Instances → interruptible, large discount; gp3 → IOPS/throughput set independently of size).

## Before finishing
Use python3 to validate: valid JSON (UTF-8, ensure_ascii False); same count and order of slugs as the input; AWS entries have names.zh/ja + definition.zh/ja; IPA entries have only ja names/definition + reading; length limits respected (shorten any that exceed).

Reply with only: terms written, and up to 3 slugs you were least sure about (with a few words why).
