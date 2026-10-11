package auth

import "github.com/gin-gonic/gin"

func Register(r *gin.Engine, svc *Service) {
	h := NewHandler(svc)

	public := r.Group("/v1/auth")
	public.POST("/signup", h.Signup)
	public.POST("/login", h.Login)
	public.POST("/refresh", h.Refresh)

	protected := r.Group("/v1")
	protected.Use(RequireAuth(svc))
	protected.POST("/auth/logout", h.Logout)
	protected.GET("/auth/sessions", h.ListSessions)
	protected.DELETE("/auth/sessions/:id", h.RevokeSession)
	protected.GET("/me", h.Me)
}
