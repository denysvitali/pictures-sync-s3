# Settings Field Audit Checklist

Use this checklist whenever adding a new field to the `Settings` struct to prevent the class of bug where a field is added to storage but not exposed through the HTTP API or JSON serialization.

## Checklist

- [ ] **Struct field added** — `Settings` struct in `settings.go` has the new field with a JSON tag.
- [ ] **UnmarshalJSON updated** — `UnmarshalJSON` in `settings.go` copies the new field from the decoded alias.
- [ ] **Default handling decided** — `applyDefaults` in `persistence.go` either applies a default or deliberately leaves the zero value.
- [ ] **Getter added** — Thread-safe getter with `RLock` in `settings.go`.
- [ ] **Setter added** — Thread-safe setter with `Lock` + `Save()` in `settings.go`.
- [ ] **Validation added** — If the field has constraints, add a `Validate*` function and call it from the setter and `Validate()`.
- [ ] **ToJSON updated** — `ToJSON()` in `settings.go` includes the new field.
- [ ] **Clone updated** — `Clone()` in `settings.go` copies the new field.
- [ ] **Request struct updated** — `settingsRequest` in `handlers/config.go` has the new pointer field with JSON tag.
- [ ] **Updater registered** — `settingsUpdaters` in `handlers/config.go` has an entry that applies the field.
- [ ] **Round-trip test updated** — `TestSettingsRoundTrip` in `settings_test.go` sets and asserts the new field.
- [ ] **Handler test updated** — `TestHandleSettingsSavesAllFields` in `config_test.go` includes the new field.
- [ ] **Lazy init checked** — If the field affects a lazily-initialized component, verify `Ensure*` is called after save.

## Prevention Mechanisms in Place

1. **Settings round-trip test** (`TestSettingsRoundTrip`) — Fails if any field does not survive `SaveTo` / `LoadFrom`.
2. **Handler all-fields test** (`TestHandleSettingsSavesAllFields`) — Fails if any field cannot be mutated via `POST /api/settings`.
3. **Partial update test** (`TestHandleSettingsPartialUpdateDoesNotResetOthers`) — Fails if an updater accidentally overwrites unrelated fields.
4. **Field dispatch registry** (`settingsUpdaters`) — All updaters live in one ordered slice; adding a field to `settingsRequest` without an updater causes silent ignore (caught by test #2).
5. **Lazy init tests** (`TestEnsureGooglePhotosClientLazyInit`) — Verify that components depending on settings are re-initialized after credential changes.
