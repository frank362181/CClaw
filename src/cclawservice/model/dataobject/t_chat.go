package dataobject

import "encoding/json"

type TSession struct {
	SessionId      string `json:"sessionId"`
	SessionName    string `json:"sessionName"`
	BeginTimeStamp int64  `json:"beginTimeStamp"`

	ChatType    int `json:"chatType"`
	ChatChannel int `json:"chatChannel"`

	Sender   string `json:"sender"`
	Archived int    `json:"archived"`

	ArchivedTimestamp int64 `json:"archivedTimestamp"`

	ChatData string `json:"chatData"`

	Removed int `json:"removed"` //是否删除的标记,0/1:否/是
}

func (session *TSession) ToJsonString() (string, error) {
	b, err := json.MarshalIndent(session, "", " ")
	if err != nil {
		return "", err
	}

	return string(b), nil
}

func (session *TSession) FromJsonString(jsonString string) error {
	err := json.Unmarshal([]byte(jsonString), session)
	if err != nil {
		return err
	}

	return nil
}

type TChat struct {
	ChatId    string `json:"chatId"`
	SessionId string `json:"sessionId"`
	Seq       int    `json:"seq"`
	ChatTime  int64  `json:"chatTime"`
	RespTime  int64  `json:"respTime"`
}

func (chat *TChat) ToJsonString() (string, error) {
	b, err := json.MarshalIndent(chat, "", " ")
	if err != nil {
		return "", err
	}

	return string(b), nil
}

func (chat *TChat) FromJsonString(jsonString string) error {
	err := json.Unmarshal([]byte(jsonString), chat)
	if err != nil {
		return err
	}

	return nil
}
