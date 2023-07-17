package adaptor

import (
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func TestDBStore(t *testing.T) {
	ast := assert.New(t)
	ctrl := gomock.NewController(t)
	adaptor := mockAdaptor(ctrl, t)

	err := adaptor.StoreState("test1", []byte("value1"))
	ast.Nil(err)
	err = adaptor.StoreState("test2", []byte("value2"))
	ast.Nil(err)
	err = adaptor.StoreState("test3", []byte("value3"))
	ast.Nil(err)

	_, err = adaptor.ReadStateSet("not found")
	ast.NotNil(err)

	ret, _ := adaptor.ReadStateSet("test")
	ast.Equal(3, len(ret))

	err = adaptor.DelState("test1")
	ast.Nil(err)
	_, err = adaptor.ReadState("test1")
	ast.NotNil(err, "not found")
	ret1, _ := adaptor.ReadState("test2")
	ast.Equal([]byte("value2"), ret1)

	err = adaptor.Destroy("t")
	ast.Nil(err)
	ret, _ = adaptor.ReadStateSet("test")
	ast.Equal(0, len(ret))
}

func TestFileStore(t *testing.T) {
	ast := assert.New(t)
	ctrl := gomock.NewController(t)
	adaptor := mockAdaptor(ctrl, t)

	err := adaptor.StoreBatchState("test1", []byte("value1"))
	ast.Nil(err)
	err = adaptor.StoreBatchState("test2", []byte("value2"))
	ast.Nil(err)
	err = adaptor.StoreBatchState("test3", []byte("value3"))
	ast.Nil(err)
	ret, _ := adaptor.ReadAllBatchState()
	ast.Equal(3, len(ret))

	err = adaptor.DelBatchState("test1")
	ast.Nil(err)
	ret1, _ := adaptor.ReadBatchState("test1")
	ast.Nil(ret1, "not found")
	ret1, _ = adaptor.ReadBatchState("test2")
	ast.Equal([]byte("value2"), ret1)

	err = adaptor.Destroy("t")
	ast.Nil(err)
	ret, _ = adaptor.ReadAllBatchState()
	ast.Equal(0, len(ret))
}
