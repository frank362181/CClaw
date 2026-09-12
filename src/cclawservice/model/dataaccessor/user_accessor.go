package dataaccessor

import (
	"errors"
	"strings"

	"gorm.io/gorm"
	exception2 "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/common/hash"
	"my.favorite/platformutility/database/core"
	"my.favorite/platformutility/exception"
)

type IUserAccessor interface {
	GetDatabase() core.IBaseDatabase

	GetUserByNameAndPassword(username, userpass string) (*dataobject.TAccount, error)
	GetUserByAccount(username string) (*dataobject.TAccount, error)
}

type UserAccessor struct {
	BaseAccessor

	gormDb *gorm.DB
}

var userAccessor = pCommon.GetSingleton(func() *UserAccessor {
	return &UserAccessor{
		BaseAccessor: BaseAccessor{
			db: nil,
		},
	}
}).Instance()

func GetUserAccessor(db core.IBaseDatabase) (IUserAccessor, error) {
	//仅仅支持gorm实现
	gormDb, ok := db.(*core.GormDatabase)
	if !ok {
		return nil, exception.UnsupportedExceptionErrorEx2("login accessor: database type[%s] is unsupported", db.GetAccessorType())
	}

	userAccessor.db = db
	userAccessor.gormDb = gormDb.GetDB()
	return userAccessor, nil
}

func (g *UserAccessor) GetUserByNameAndPassword(username, userpass string) (*dataobject.TAccount, error) {
	username = strings.ToLower(username)
	username = strings.TrimSpace(username)
	if len(username) == 0 {
		return nil, exception.ParameterEmptyErrorEx("username must not be empty")
	}

	var user dataobject.TAccount
	err := g.gormDb.First(&user, "login_name = ?", username).Error
	if err != nil {
		//找不到用户
		return nil, exception.UnMatchErrorEx2("username=%s can not find", username)
	}

	encryptedPass := hash.HmacSha256(user.LoginKey, userpass)
	// 恒定时间比较,避免时序侧信道泄露 LoginKey/LoginPass 信息
	if hash.HmacEqual([]byte(encryptedPass), []byte(user.LoginPass)) {
		return &user, nil
	}

	return nil, exception2.UserPasswordUnmatchEx("username and password do not match")
}

func (g *UserAccessor) GetUserByAccount(username string) (*dataobject.TAccount, error) {
	username = strings.ToLower(username)
	username = strings.TrimSpace(username)
	if len(username) == 0 {
		return nil, exception.ParameterEmptyErrorEx("username must not be empty")
	}

	var user dataobject.TAccount
	err := g.gormDb.First(&user, "login_name = ?", username).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil // 未命中,约定返回(nil,nil),由调用方判空
	}

	if err != nil {
		return nil, err // 真实db错误透传
	}

	return &user, nil
}
