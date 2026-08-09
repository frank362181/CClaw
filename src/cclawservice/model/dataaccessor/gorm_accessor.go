package dataaccessor

import (
	"strings"

	exception2 "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/platformutility/common/hash"
	"my.favorite/platformutility/database/core"
	"my.favorite/platformutility/exception"
)

func (g *GormAccessor) GetUserByNameAndPassword(username, userpass string) (*dataobject.TAccount, error) {
	username = strings.ToLower(username)
	username = strings.TrimSpace(username)
	if len(username) == 0 {
		return nil, exception.ParameterEmptyErrorEx("username must not be empty")
	}

	gormDb := g.db.(*core.GormDatabase)
	var user dataobject.TAccount
	err := gormDb.GetDB().First(&user, "login_name = ?", username).Error
	if err != nil {
		//找不到用户
		return nil, exception.UnMatchErrorEx2("username=%s can not find", username)
	}

	encryptedPass := hash.HmacSha256(user.LoginKey, userpass)
	if encryptedPass == user.LoginPass {
		return &user, nil
	}

	return nil, exception2.UserPasswordUnmatchEx("username and password do not match")
}

func (g *GormAccessor) GetUserByAccount(username string) (*dataobject.TAccount, error) {
	username = strings.ToLower(username)
	username = strings.TrimSpace(username)
	if len(username) == 0 {
		return nil, exception.ParameterEmptyErrorEx("username must not be empty")
	}

	gormDb := g.db.(*core.GormDatabase)
	var user dataobject.TAccount
	err := gormDb.GetDB().First(&user, "login_name = ?", username).Error

	return &user, err
}
