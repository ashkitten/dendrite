package routing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/matrix-org/gomatrixserverlib"
	"github.com/matrix-org/util"
	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"

	"github.com/matrix-org/dendrite/clientapi/jsonerror"
	"github.com/matrix-org/dendrite/internal/httputil"
	"github.com/matrix-org/dendrite/keyserver/api"
	roomserverAPI "github.com/matrix-org/dendrite/roomserver/api"
	"github.com/matrix-org/dendrite/setup/config"
	"github.com/matrix-org/dendrite/setup/jetstream"
	userapi "github.com/matrix-org/dendrite/userapi/api"
)

func AdminEvacuateRoom(req *http.Request, cfg *config.ClientAPI, device *userapi.Device, rsAPI roomserverAPI.ClientRoomserverAPI) util.JSONResponse {
	vars, err := httputil.URLDecodeMapValues(mux.Vars(req))
	if err != nil {
		return util.ErrorResponse(err)
	}
	roomID, ok := vars["roomID"]
	if !ok {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: jsonerror.MissingArgument("Expecting room ID."),
		}
	}
	res := &roomserverAPI.PerformAdminEvacuateRoomResponse{}
	if err := rsAPI.PerformAdminEvacuateRoom(
		req.Context(),
		&roomserverAPI.PerformAdminEvacuateRoomRequest{
			RoomID: roomID,
		},
		res,
	); err != nil {
		return util.ErrorResponse(err)
	}
	if err := res.Error; err != nil {
		return err.JSONResponse()
	}
	return util.JSONResponse{
		Code: 200,
		JSON: map[string]interface{}{
			"affected": res.Affected,
		},
	}
}

func AdminEvacuateUser(req *http.Request, cfg *config.ClientAPI, device *userapi.Device, rsAPI roomserverAPI.ClientRoomserverAPI) util.JSONResponse {
	vars, err := httputil.URLDecodeMapValues(mux.Vars(req))
	if err != nil {
		return util.ErrorResponse(err)
	}
	userID, ok := vars["userID"]
	if !ok {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: jsonerror.MissingArgument("Expecting user ID."),
		}
	}
	_, domain, err := gomatrixserverlib.SplitID('@', userID)
	if err != nil {
		return util.MessageResponse(http.StatusBadRequest, err.Error())
	}
	if domain != cfg.Matrix.ServerName {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: jsonerror.MissingArgument("User ID must belong to this server."),
		}
	}
	res := &roomserverAPI.PerformAdminEvacuateUserResponse{}
	if err := rsAPI.PerformAdminEvacuateUser(
		req.Context(),
		&roomserverAPI.PerformAdminEvacuateUserRequest{
			UserID: userID,
		},
		res,
	); err != nil {
		return jsonerror.InternalAPIError(req.Context(), err)
	}
	if err := res.Error; err != nil {
		return err.JSONResponse()
	}
	return util.JSONResponse{
		Code: 200,
		JSON: map[string]interface{}{
			"affected": res.Affected,
		},
	}
}

func AdminPurgeRoom(req *http.Request, cfg *config.ClientAPI, device *userapi.Device, rsAPI roomserverAPI.ClientRoomserverAPI) util.JSONResponse {
	vars, err := httputil.URLDecodeMapValues(mux.Vars(req))
	if err != nil {
		return util.ErrorResponse(err)
	}
	roomID, ok := vars["roomID"]
	if !ok {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: jsonerror.MissingArgument("Expecting room ID."),
		}
	}
	res := &roomserverAPI.PerformAdminPurgeRoomResponse{}
	if err := rsAPI.PerformAdminPurgeRoom(
		context.Background(),
		&roomserverAPI.PerformAdminPurgeRoomRequest{
			RoomID: roomID,
		},
		res,
	); err != nil {
		return util.ErrorResponse(err)
	}
	if err := res.Error; err != nil {
		return err.JSONResponse()
	}
	return util.JSONResponse{
		Code: 200,
		JSON: res,
	}
}

func AdminResetPassword(req *http.Request, cfg *config.ClientAPI, device *userapi.Device, userAPI userapi.ClientUserAPI) util.JSONResponse {
	vars, err := httputil.URLDecodeMapValues(mux.Vars(req))
	if err != nil {
		return util.ErrorResponse(err)
	}
	localpart, ok := vars["localpart"]
	if !ok {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: jsonerror.MissingArgument("Expecting user localpart."),
		}
	}
	request := struct {
		Password string `json:"password"`
	}{}
	if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: jsonerror.Unknown("Failed to decode request body: " + err.Error()),
		}
	}
	if request.Password == "" {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: jsonerror.MissingArgument("Expecting non-empty password."),
		}
	}
	updateReq := &userapi.PerformPasswordUpdateRequest{
		Localpart:     localpart,
		Password:      request.Password,
		LogoutDevices: true,
	}
	updateRes := &userapi.PerformPasswordUpdateResponse{}
	if err := userAPI.PerformPasswordUpdate(req.Context(), updateReq, updateRes); err != nil {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: jsonerror.Unknown("Failed to perform password update: " + err.Error()),
		}
	}
	return util.JSONResponse{
		Code: http.StatusOK,
		JSON: struct {
			Updated bool `json:"password_updated"`
		}{
			Updated: updateRes.PasswordUpdated,
		},
	}
}

func AdminReindex(req *http.Request, cfg *config.ClientAPI, device *userapi.Device, natsClient *nats.Conn) util.JSONResponse {
	_, err := natsClient.RequestMsg(nats.NewMsg(cfg.Matrix.JetStream.Prefixed(jetstream.InputFulltextReindex)), time.Second*10)
	if err != nil {
		logrus.WithError(err).Error("failed to publish nats message")
		return jsonerror.InternalServerError()
	}
	return util.JSONResponse{
		Code: http.StatusOK,
		JSON: struct{}{},
	}
}

func AdminMarkAsStale(req *http.Request, cfg *config.ClientAPI, keyAPI api.ClientKeyAPI) util.JSONResponse {
	vars, err := httputil.URLDecodeMapValues(mux.Vars(req))
	if err != nil {
		return util.ErrorResponse(err)
	}
	userID := vars["userID"]

	_, domain, err := gomatrixserverlib.SplitID('@', userID)
	if err != nil {
		return util.MessageResponse(http.StatusBadRequest, err.Error())
	}
	if domain == cfg.Matrix.ServerName {
		return util.JSONResponse{
			Code: http.StatusBadRequest,
			JSON: jsonerror.InvalidParam("Can not mark local device list as stale"),
		}
	}

	err = keyAPI.PerformMarkAsStaleIfNeeded(req.Context(), &api.PerformMarkAsStaleRequest{
		UserID: userID,
		Domain: domain,
	}, &struct{}{})
	if err != nil {
		return util.JSONResponse{
			Code: http.StatusInternalServerError,
			JSON: jsonerror.Unknown(fmt.Sprintf("Failed to mark device list as stale: %s", err)),
		}
	}
	return util.JSONResponse{
		Code: http.StatusOK,
		JSON: struct{}{},
	}
}
