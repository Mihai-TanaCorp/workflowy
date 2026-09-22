#!/usr/bin/env bats

load test_helper

setup() {
    [[ -x "$WORKFLOWY_BIN" ]] || skip "Build the CLI first"
    skip_if_no_api_key
    skip_if_no_test_parent
    require_jq
    sandbox_id=""
}

wf() {
    "$WORKFLOWY_BIN" --api-key-file "$API_KEY_FILE" --format=json "$@"
}

teardown() {
    if [[ -n "$sandbox_id" ]]; then
        wf delete "$sandbox_id"
    fi
}

@test "live mirror lifecycle preserves the original and resolves mirror origins" {
    run wf create --parent-id "$TEST_PARENT_ID" --name "CLI mirror integration sandbox"
    [ "$status" -eq 0 ]
    sandbox_id=$(printf '%s' "$output" | jq -er .item_id)
    [ -n "$sandbox_id" ]

    run wf create --parent-id "$sandbox_id" --name "Original" --note "Clear me"
    [ "$status" -eq 0 ]
    original_id=$(printf '%s' "$output" | jq -er .item_id)

    run wf mirror "$original_id" "$sandbox_id" --position bottom
    [ "$status" -eq 0 ]
    mirror_id=$(printf '%s' "$output" | jq -er .item_id)
    [ "$(printf '%s' "$output" | jq -r .origin_id)" = "$original_id" ]

    run wf mirror "$mirror_id" "$sandbox_id"
    [ "$status" -eq 0 ]
    second_id=$(printf '%s' "$output" | jq -er .item_id)
    [ "$(printf '%s' "$output" | jq -r .origin_id)" = "$original_id" ]

    run wf update "$original_id" --note ""
    [ "$status" -eq 0 ]
    run wf delete-mirror "$mirror_id"
    [ "$status" -eq 0 ]
    run wf delete-mirror "$second_id"
    [ "$status" -eq 0 ]
    run wf delete-mirror "$original_id"
    [ "$status" -ne 0 ]

    run wf get "$original_id" --method=get --depth=1
    [ "$status" -eq 0 ]
    printf '%s' "$output" | jq -e --arg id "$original_id" \
        '.id == $id and .name == "Original" and (.note == "" or .note == null)'
}
