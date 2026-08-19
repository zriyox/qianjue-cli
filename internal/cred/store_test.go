package cred

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func deviceRecord() *Record {
	return &Record{
		CredentialType:       TypeDeviceFlow,
		AccessToken:          "qj_at_test",
		AccessTokenExpiresAt: time.Now().Add(2 * time.Hour),
		RefreshToken:         "qj_rt_test",
		SessionExpiresAt:     time.Now().Add(30 * 24 * time.Hour),
		SessionID:            "qj_ds_abc",
		Scopes:               []string{"task.create", "task.read"},
	}
}

func TestAccountNames(t *testing.T) {
	assert.Equal(t, "local:device-flow", DeviceFlowAccount("local"))
	assert.Equal(t, "local:device-flow.staging", DeviceFlowStagingAccount("local"))
	assert.Equal(t, "local:pat", PATAccount("local"))
}

func TestMemoryStoreContract(t *testing.T) {
	s := NewMemoryStore()

	_, err := s.Get("local:device-flow")
	assert.ErrorIs(t, err, ErrNotFound)

	rec := deviceRecord()
	require.NoError(t, s.Set(DeviceFlowAccount("local"), rec))

	got, err := s.Get(DeviceFlowAccount("local"))
	require.NoError(t, err)
	assert.Equal(t, rec.AccessToken, got.AccessToken)
	assert.Equal(t, rec.Scopes, got.Scopes)

	// 深拷贝：修改取出的记录不影响存储
	got.Scopes[0] = "mutated"
	again, err := s.Get(DeviceFlowAccount("local"))
	require.NoError(t, err)
	assert.Equal(t, "task.create", again.Scopes[0])

	require.NoError(t, s.Delete(DeviceFlowAccount("local")))
	_, err = s.Get(DeviceFlowAccount("local"))
	assert.ErrorIs(t, err, ErrNotFound)

	// 删除不存在的记录不是错误
	require.NoError(t, s.Delete("ghost"))
}

func TestRecordValidate(t *testing.T) {
	require.NoError(t, deviceRecord().Validate())
	require.NoError(t, (&Record{CredentialType: TypePAT, AccessToken: "qj_pat_x"}).Validate())

	assert.Error(t, (&Record{CredentialType: TypeDeviceFlow, AccessToken: "qj_at_x"}).Validate(), "缺 refresh token")
	assert.Error(t, (&Record{CredentialType: TypePAT}).Validate(), "缺 token")
	assert.Error(t, (&Record{CredentialType: "OTHER", AccessToken: "x"}).Validate(), "未知类型")

	s := NewMemoryStore()
	assert.Error(t, s.Set("a", &Record{CredentialType: TypePAT}), "非法记录不得入库")
}

func TestMemoryStoreFailWith(t *testing.T) {
	s := NewMemoryStore()
	s.FailWith = assert.AnError
	_, err := s.Get("x")
	assert.ErrorIs(t, err, assert.AnError)
	assert.ErrorIs(t, s.Set("x", deviceRecord()), assert.AnError)
	assert.ErrorIs(t, s.Delete("x"), assert.AnError)
}
