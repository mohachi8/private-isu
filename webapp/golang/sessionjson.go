package main

import (
	"encoding/json"
	"fmt"
)

// jsonSessionSerializer replaces securecookie's default Gob serializer. Gob
// recompiles the type descriptor on every Encode/Decode (≈10% CPU here since the
// session is read on every request); JSON over the small string-keyed session
// (user_id, csrf_token, notice) avoids that.
type jsonSessionSerializer struct{}

func (jsonSessionSerializer) Serialize(src interface{}) ([]byte, error) {
	m, ok := src.(map[interface{}]interface{})
	if !ok {
		return json.Marshal(src)
	}
	sm := make(map[string]interface{}, len(m))
	for k, v := range m {
		ks, ok := k.(string)
		if !ok {
			ks = fmt.Sprint(k)
		}
		sm[ks] = v
	}
	return json.Marshal(sm)
}

func (jsonSessionSerializer) Deserialize(b []byte, dst interface{}) error {
	dm, ok := dst.(*map[interface{}]interface{})
	if !ok {
		return json.Unmarshal(b, dst)
	}
	sm := map[string]interface{}{}
	if err := json.Unmarshal(b, &sm); err != nil {
		return err
	}
	if *dm == nil {
		*dm = make(map[interface{}]interface{}, len(sm))
	}
	for k, v := range sm {
		(*dm)[k] = v
	}
	return nil
}
