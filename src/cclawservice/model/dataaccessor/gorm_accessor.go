package dataaccessor

import (
	"strings"

	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/platformutility/database/core"
	"my.favorite/platformutility/exception"
)

func (g *GormAccessor) GetUserByNameAndPassword(username, userpass string) (*dataobject.T_Account, error) {
	username = strings.ToLower(username)
	username = strings.TrimSpace(username)
	if len(username) == 0 {
		return nil, exception.ParameterEmptyErrorEx("username must not be empty")
	}

	gormDb := g.db.(*core.GormDatabase)
	var user dataobject.T_Account
	err := gormDb.GetDB().First(&user, "login_name = ? and login_pass = ?", username, userpass).Error

	return &user, err
}

func (g *GormAccessor) GetUserByAccount(username string) (*dataobject.T_Account, error) {
	username = strings.ToLower(username)
	username = strings.TrimSpace(username)
	if len(username) == 0 {
		return nil, exception.ParameterEmptyErrorEx("username must not be empty")
	}

	gormDb := g.db.(*core.GormDatabase)
	var user dataobject.T_Account
	err := gormDb.GetDB().First(&user, "login_name = ?", username).Error

	return &user, err
}
