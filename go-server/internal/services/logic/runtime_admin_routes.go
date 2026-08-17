package logic

import (
	"context"
	"errors"
	"net/http"

	"longheng.io/server/internal/platform/admincmd"
	"longheng.io/server/internal/runtime/notice"
	"longheng.io/server/internal/runtime/redeem"
)

// AdminRouteRegistrar 定义 runtime 向 logic admin mux 注册路由的函数。
type AdminRouteRegistrar func(*Service, *http.ServeMux) error

// NoticeAdminRouteRegistrar 返回公告 runtime 的 admin 路由注册器。
func NoticeAdminRouteRegistrar(notices notice.Service) AdminRouteRegistrar {
	return func(s *Service, mux *http.ServeMux) error {
		if notices.Store == nil {
			return nil
		}
		prefix := s.adminPrefix + "/notice"
		return notice.RegisterAdminRoutes(mux, notices, notice.AdminOptions{
			Prefix: prefix,
			Wrap:   s.admin,
			MutateWrap: func(operation string, next http.HandlerFunc) http.HandlerFunc {
				return s.adminDangerous(operation, next)
			},
			CommandHooks: notice.AdminCommandHooks{
				Submit: func(r *http.Request, operation, target, reason string, params any) (admincmd.Receipt, bool, bool, error) {
					return s.submitOpsCmd(r, operation, target, reason, params)
				},
				MarkSucceeded: func(ctx context.Context, receipt admincmd.Receipt) admincmd.Receipt {
					return s.markOpsAdminOK(ctx, receipt)
				},
				MarkFailed: func(ctx context.Context, receipt admincmd.Receipt, err error) {
					s.markOpsCmdFail(ctx, receipt, err)
				},
				WriteError:           writeOpsCmdErr,
				WriteDuplicate:       writeOpsDup,
				WriteJSONWithReceipt: writeOpsAdminReceipt,
			},
		})
	}
}

// RedeemAdminRouteRegistrar 返回兑换码 runtime 的 admin 路由注册器。
func RedeemAdminRouteRegistrar(redeems redeem.Service) AdminRouteRegistrar {
	return func(s *Service, mux *http.ServeMux) error {
		if redeems.Store == nil {
			return nil
		}
		prefix := s.adminPrefix + "/redeem"
		return redeem.RegisterAdminRoutes(mux, redeems, redeem.AdminOptions{
			Prefix: prefix,
			Wrap:   s.admin,
			MutateWrap: func(operation string, next http.HandlerFunc) http.HandlerFunc {
				return s.adminDangerous(operation, next)
			},
			CommandHooks: redeem.AdminCommandHooks{
				Submit: func(r *http.Request, operation, target, reason string, params any) (admincmd.Receipt, bool, bool, error) {
					return s.submitOpsCmd(r, operation, target, reason, params)
				},
				MarkSucceeded: func(ctx context.Context, receipt admincmd.Receipt) admincmd.Receipt {
					return s.markOpsAdminOK(ctx, receipt)
				},
				MarkFailed: func(ctx context.Context, receipt admincmd.Receipt, err error) {
					s.markOpsCmdFail(ctx, receipt, err)
				},
				WriteError:           writeOpsCmdErr,
				WriteDuplicate:       writeOpsDup,
				WriteJSONWithReceipt: writeOpsAdminReceipt,
			},
		})
	}
}

func (s *Service) registerAdminRoutes(mux *http.ServeMux) {
	for _, register := range s.adminRoutes {
		if register == nil {
			continue
		}
		if err := register(s, mux); err != nil {
			s.initErr = errors.Join(s.initErr, err)
		}
	}
}
