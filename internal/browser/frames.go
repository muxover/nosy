package browser

import (
	"context"
	"encoding/json"
	"fmt"
)

// typeIntoFrames types the test string into the fields of the frames that
// run in other processes, the way typeIntoFields does for the page, over each
// frame's own session. It returns how many fields were typed into.
func (v *visit) typeIntoFrames(ctx context.Context) int {
	if v.dt == nil {
		return 0
	}
	typed := 0
	for _, session := range v.dt.frameSessions() {
		res, err := v.dt.call(ctx, session, "Runtime.evaluate", map[string]any{
			"expression": fmt.Sprintf(fieldsJS, maxFields), "returnByValue": true,
		})
		if err != nil {
			v.logf("find fields in a frame: %v", err)
			continue
		}
		var found struct {
			Result struct {
				Value int `json:"value"`
			} `json:"result"`
		}
		if json.Unmarshal(res, &found) != nil {
			continue
		}
		for i := 0; i < found.Result.Value && ctx.Err() == nil; i++ {
			if v.typeInFrameField(ctx, session, i) == nil {
				typed++
			}
		}
	}
	return typed
}

func (v *visit) typeInFrameField(ctx context.Context, session string, i int) error {
	d := v.dt
	if _, err := d.call(ctx, session, "Runtime.evaluate", map[string]any{
		"expression": fmt.Sprintf(`document.querySelector('[data-nosy-field="%d"]').focus()`, i),
	}); err != nil {
		return err
	}
	for _, ch := range v.o.Token {
		s := string(ch)
		if _, err := d.call(ctx, session, "Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "text": s, "key": s}); err != nil {
			return err
		}
		if _, err := d.call(ctx, session, "Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": s}); err != nil {
			return err
		}
	}
	return nil
}
