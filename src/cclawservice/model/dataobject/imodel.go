/*
* data model的接口操作,可被所有的data model实现
**/

package dataobject

type IDataModel interface {
	ToJsonString() string
	FromJsonString(jsonStr string) error
}
