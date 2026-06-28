package auditlog

const (
	ActionAuthRegister          = "auth.register"
	ActionAuthLogin             = "auth.login"
	ActionAuthLogout            = "auth.logout"
	ActionAccountSwitchIdentity = "account.switch_identity"

	ActionEnterpriseCreate        = "enterprise.create"
	ActionEnterpriseInviteCreate  = "enterprise.invite.create"
	ActionEnterpriseMemberRemove  = "enterprise.member.remove"
	ActionEnterpriseOwnerTransfer = "enterprise.owner.transfer"

	ActionAdminBootstrap      = "admin.bootstrap"
	ActionAdminAuthLogin      = "admin.auth.login"
	ActionAdminAuthLogout     = "admin.auth.logout"
	ActionAdminPasswordRotate = "admin.password.rotate"
	ActionAdminCreate         = "admin.create"
	ActionAdminDisable        = "admin.disable"
	ActionUserStatusSet       = "user.status.set"

	ActionProjectCreate      = "project.create"
	ActionProjectUpdate      = "project.update"
	ActionProjectAssetAttach = "project.asset.attach"
	ActionProjectArchive     = "project.archive"
	ActionProjectRestore     = "project.restore"

	ActionWorkCreate         = "work.create"
	ActionWorkUpdate         = "work.update"
	ActionWorkShare          = "work.share"
	ActionWorkUnshare        = "work.unshare"
	ActionWorkPublicTakeDown = "work.public.take_down"
	ActionWorkLike           = "work.like"
)

var businessActions = []string{
	ActionAuthRegister,
	ActionAuthLogin,
	ActionAuthLogout,
	ActionAccountSwitchIdentity,
	ActionEnterpriseCreate,
	ActionEnterpriseInviteCreate,
	ActionEnterpriseMemberRemove,
	ActionEnterpriseOwnerTransfer,
	ActionAdminBootstrap,
	ActionAdminAuthLogin,
	ActionAdminAuthLogout,
	ActionAdminPasswordRotate,
	ActionAdminCreate,
	ActionAdminDisable,
	ActionUserStatusSet,
	ActionProjectCreate,
	ActionProjectUpdate,
	ActionProjectAssetAttach,
	ActionProjectArchive,
	ActionProjectRestore,
	ActionWorkCreate,
	ActionWorkUpdate,
	ActionWorkShare,
	ActionWorkUnshare,
	ActionWorkPublicTakeDown,
	ActionWorkLike,
}

func BusinessActionValues() []string {
	out := make([]string, len(businessActions))
	copy(out, businessActions)
	return out
}

func KnownBusinessAction(action string) bool {
	for _, known := range businessActions {
		if action == known {
			return true
		}
	}
	return false
}
