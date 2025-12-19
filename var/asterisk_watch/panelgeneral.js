{{define "appcode"}}
<script>
var layout = {{.Layout.LayoutType}};
var globalOrgID = '{{.AstOrgID}}';
var lastServerInst = '';
var globalPanelID = '{{.PanelID}}';

function updateValue(elem, newVal) {
    var oldValue = elem.text();
    if (oldValue != newVal) {
        var oldBackground = elem.css('background-color');
        elem.stop().css('background-color', '#2cb5b5').animate({backgroundColor: '#ffffff'}, 10000);
    }
    $(elem).text(newVal)
}

function formatSecs(secs) {
    var d = Math.floor(secs / 86400);
    secs %= 86400;
    var h = Math.floor(secs / 3600);
    secs %= 3600;
    var m = Math.floor(secs / 60);
    secs = Math.floor(secs % 60);
    if (d < 10) {
        if (d > 0)
            d = '0' + d + ' ';
        else
            d = '';
    } else
        d = d + ' ';
    return d + ((h < 10) ? '0' + h : h)+ ':' +
           ((m < 10) ? '0' + m : m) + ':' +
           ((secs < 10) ? '0' + secs : secs);
}

function formatDelta(millisecs) {
    var secs = Math.floor(millisecs / 1000);
    var d = Math.floor(secs / 86400);
    secs %= 86400;
    var h = Math.floor(secs / 3600);
    secs %= 3600;
    var m = Math.floor(secs / 60);
    secs = Math.floor(secs % 60);
    if (d < 10) {
        if (d > 0)
            d = '0' + d + ' ';
        else
            d = '';
    } else
        d = d + ' ';
    return d + ((h < 10) ? '0' + h : h)+ ':' +
           ((m < 10) ? '0' + m : m) + ':' +
           ((secs < 10) ? '0' + secs : secs);
}

function updateQueueStats(evt) {
    var qDiv = $('.grid-item-queue[data-qn="'+evt.queuename+'"]');
    if (!qDiv)
        return;
    updateValue($('.queuestatvalue[data-qn="'+evt.queuename+'"][data-id="whenstatscleared"]'), evt.whenstatscleared.toString('MM/dd hh:mmtt'));
    updateValue($('.queuestatvalue[data-qn="'+evt.queuename+'"][data-id="calls"]'), evt.calls);
    updateValue($('.queuestatvalue[data-qn="'+evt.queuename+'"][data-id="completed"]'), evt.completed);
    updateValue($('.queuestatvalue[data-qn="'+evt.queuename+'"][data-id="abandoned"]'), evt.abandoned);
    updateValue($('.queuestatvalue[data-qn="'+evt.queuename+'"][data-id="slp"]'), evt.servicelevelperf+"%");
    updateValue($('.queuestatvalue[data-qn="'+evt.queuename+'"][data-id="avgtalk"]'), evt.talktime);
    updateValue($('.queuestatvalue[data-qn="'+evt.queuename+'"][data-id="avghold"]'), evt.holdtime);
}

function updateAgentStatus(queuename, agentID, status, paused, callstaken, penalty) {
    var agentdiv = $('div[data-qn="'+queuename+'"]').find('div[data-id="'+agentID+'"]');
    /*
        0 - AST_DEVICE_UNKNOWN
        1 - AST_DEVICE_NOT_INUSE
        2 - AST_DEVICE_INUSE
        3 - AST_DEVICE_BUSY
        4 - AST_DEVICE_INVALID
        5 - AST_DEVICE_UNAVAILABLE
        6 - AST_DEVICE_RINGING
        7 - AST_DEVICE_RINGINUSE
        8 - AST_DEVICE_ONHOLD
    */
    var c = 'label agentIcon agentUnavail';
    var h = 'U';
    switch (status) {
        case 1: c='label agentIcon agentIdle'; h='A'; break;
        case 2:
        case 8:
        case 3: c='label agentIcon agentBusy'; h='B'; break;
        case 6:
        case 7: c='label agentIcon agentRing'; h='R'; break;
    }
    if (paused) {
        c = 'label agentIcon agentPaused';
        h = 'P'
    }
    if (agentdiv) {
        $(agentdiv).find('span.agentIcon').attr('class', c).html(h);
        $(agentdiv).find('span.agentPenalty').text('['+penalty+']');
        $(agentdiv).find('span.callsTaken').text('('+callstaken+')');
    }
}

function initAgentStatus() {
    {{if ne .Layout.LayoutType 0}}{{- range $idx, $q := .Queues -}}
        {{range $loc, $val := .QI.Members}}updateAgentStatus('{{$q.Name}}','{{$loc}}', {{$val.Status}}, {{$val.Paused}}, {{$val.CallsTaken}}, {{$val.Penalty}});
        {{end}}
    {{- end -}}{{end}}
}

function timerClocks() {
    $('.callerWhenEntered').each(function(i, e) {
        var secs = Number.parseInt($(this).attr('data-secs'));
        secs += 1;
        $(this).attr('data-secs', secs).html(formatSecs(secs));
    });
    var now = Date.now();
    $('.grid-item-hint').each(function (i, e) {
        var s = $(this).attr('data-status');
        $(this).find('.statusTimer').each(function(i, e) {
            var msecs = Number.parseInt($(this).attr('data-time'));
            if (s > 0 && s != 4)
                $(this).html(formatDelta(now - msecs));
            else
                $(this).html('&nbsp;');
        });
    });
    // look for and update parked calls
    var pcalls = [
        {{range $index, $call := .ParkedCalls}}
        {{if $index}},{{end}}
        {
            ParkeeChannel: "{{$call.ParkeeChannel}}",
            ParkingDuration: {{$call.ParkingDuration}}
        }
        {{end}}
    ];
    console.log(typeof pcalls);
    console.log(pcalls," - ",Array.isArray(pcalls));
    if (Array.isArray(pcalls)) {
        pcalls.forEach(function (p, index) {
            console.log("Processing parked call for channel "+p.ParkeeChannel);
            let parkDiv = $('[data-id="' + p.ParkeeChannel + '"]');
            console.log("parked call div: ", parkDiv);
            if (parkDiv.length > 0) {
                let timeStr = parkDiv.attr('data-time');
                let offsetSecs = parseInt(parkDiv.attr('data-duration')) || 0;
                console.log("offsetSecs: ", offsetSecs);
                // Extract "2025-12-19 11:18:48" part
                let dateStr = timeStr.substring(0, 19).replace(' ', 'T');
                let parkedTime = new Date(dateStr);
                let now = new Date();
                let durationSeconds = Math.floor((now - parkedTime) / 1000);
                console.log("Parked duration seconds BEFORE: ", durationSeconds);
                durationSeconds += offsetSecs;
                console.log("Parked duration seconds AFTER: ", durationSeconds);
                // parkDiv.attr('data-duration', durationSeconds);
                parkDiv.text(formatSecs(durationSeconds));
            }
        });
    }

    setTimeout(timerClocks, 1000);
}

function enableVarInput(state) {
    state = !state;
    {{range .Vars}}
    $('[name="VAR_{{.Name}}"]').prop('disabled', state);
    {{end}}
}

function getVars() {
    $.ajax({
        type: "GET",
        dataType: "json",
        url: "/vars/get/{{.PanelID}}",
        success: function(data, textStatus){
            // update all variables on-screen to match current values
            data.forEach(function (v, index) {
                $('input[name="VAR_'+v.name+'"][value="'+v.val+'"]').prop('checked', true);
            });
            enableVarInput(true);
        },
        error: function(jqXHR, textStatus, errorThrown){
            console.log(textStatus + ":" + errorThrown);
        }
    });
}

function togglePBXControls() {
    if ($('#grid-vars').is(':hidden')) {
        $('#varsMenu').parent('li').addClass('active');
        $('#grid-vars').slideDown();
        getVars();
    } else {
        $('#varsMenu').parent('li').removeClass('active');
        $('#grid-vars').slideUp();
        enableVarInput(false);
    }
}

function setVar(name, value) {
    $.ajax({
        type: "POST",
        dataType: "json",
        data: JSON.stringify({'name':name, 'val':value}),
        url: "/vars/set/{{.PanelID}}",
        success: function(data, textStatus){
            var element = $('p[var="VAR_'+name+'"]');
            element.slideDown();
            setTimeout(function () {element.slideUp();}, 3000);
        },
        error: function(jqXHR, textStatus, errorThrown){
            alert("Error changing "+name);
        }
    });
}

$(document).ready(function(){
    var wsconn;
    console.log({{.}});
    function connectWS() {
        wsconn = new WebSocket('wss://' + window.location.host + '/ws');
        wsconn.onopen = function(e) {
            //console.log("WSOPEN");
            socketConnected = true;
            $('.isoffline').hide();
            wsconn.send(JSON.stringify({cmd:'WATCHORG', orgid:globalOrgID}));
        }
        wsconn.onerror = function(e) {
            console.log("WSERROR");
            console.log(e);
            socketConnected = false;
            $('.isoffline').show();
            var t = 5;
            retryTimer = setInterval(function() {
                t = t - 1;
                if (t == 0) {
                    clearInterval(retryTimer);
                    setTimeout(connectWS, 0);
                } else
                    $('#retryCountdown').text(t)
            }, 1000);
        }
        wsconn.onclose = function(e) {
            //console.log("WSCLOSE");
            socketConnected = false;
            $('.isoffline').show();
            var t = 5;
            retryTimer = setInterval(function() {
                t = t - 1;
                if (t == 0) {
                    clearInterval(retryTimer);
                    setTimeout(connectWS, 0);
                } else
                    $('#retryCountdown').text(t)
            }, 1000);
        }
        wsconn.onmessage = function(e) {
            var hcn = JSON.parse(e.data);
            //console.log(hcn);
            switch (hcn.event) {
                case 'WATCHORG':
                    if (hcn.result != 'OK') {
                        $('body').html("Access Denied");
                        wsconn.close();
                    } else {
                        if (lastServerInst.length > 0 && hcn.serverinstance != lastServerInst) {
                            // We just connected to the server but it has been restarted, so
                            // let's force a reload.
                            wsconn.close();
                            window.location.reload(true);
                        } else {
                            lastServerInst = hcn.serverinstance;
                        }
                    }
                    break;
                case 'LAYOUTCHANGE':
                    if (hcn.panelid == globalPanelID)
                        window.location.reload(true);
                    break;
                case 'RELOAD':
                    window.location.reload(true);
                    break;
                case 'HINTCHANGE':
                    var x = $('.grid-item-hint[data-exten="'+hcn.exten+'"]');
                    var oldStat = $(x).attr('data-status');
                    var status = $(x).find('.statusTextCID');
                    if (status.length > 0) {
                        if (hcn.statusstr == 'In-Use') {
                            $(status).addClass("smallText").text(hcn.calleridname);
                        } else
                            $(status).removeClass("smallText").html(hcn.statusstr);
                    } else {
                        $(x).find('.statusText').removeClass("smallText").html(hcn.statusstr);
                    }
                    if (hcn.status < 0)
                        $(x).removeClass('status-'+oldStat).addClass('status-deactivated');
                    else
                        $(x).removeClass('status-'+oldStat).addClass('status-'+hcn.status);
                    $(x).attr('data-status', hcn.status);
                    hcn.laststatuschange = Date.now();   // substitute with our own clock in case the server clock and the client clocks aren't both correct
                    $(x).find('.statusTimer').attr('data-time', hcn.laststatuschange).html(formatDelta(Date.now() - hcn.laststatuschange));
                    // console.log((Date.now() - hcn.laststatuschange)+';'+formatDelta(Date.now() - hcn.laststatuschange));
                    $(x).find('.statusCID').text(`${hcn.calleridname}`);
                    break;
                case 'AGENTCHANGE':
                    hcn.whenadded = new XDate(hcn.whenadded)
                    hcn.lastcall = new XDate(hcn.lastcall)
                    updateAgentStatus(hcn.queuename, hcn.location, hcn.status, hcn.paused, hcn.callstaken, hcn.penalty);
                    break;
                case 'AGENTADD':
                    addAgent(hcn);
                    updateAgentStatus(hcn.queuename, hcn.location, hcn.status, hcn.paused, hcn.callstaken, hcn.penalty);
                    break;
                case 'AGENTREMOVE':
                    {   var agentdiv = $('div[data-qn="'+hcn.queuename+'"]').find('div[data-id="'+hcn.location+'"]');
                        if (agentdiv) agentdiv.remove();
                    }
                    break;
                case 'QUEUECALLERSCHANGE':
                    emptyCallers(hcn.queuename);
                    for (i = 0; i < hcn.callers.length; i++) {
                        hcn.callers[i].whenentered = new XDate(hcn.callers[i].whenentered)
                        addCaller(hcn.queuename, hcn.callers[i]);
                    }
                    // fall through
                case 'QUEUECHANGE':
                    hcn.whenstatscleared = new XDate(hcn.whenstatscleared)
                    updateQueueStats(hcn);
                    break;
            }
        }
    }
    initAgentStatus();
    connectWS();
    setTimeout(timerClocks, 1000);

    {{range .Vars}}
    $('input[type=radio][name="VAR_{{.Name}}"]').change(function() {
        {{$name := .Name}}
        {{range $index, $element := .Values}}
        if (this.value === '{{$element}}') {
            setVar('{{$name}}', '{{$element}}');
        }
        {{end}}
    });
    {{end}}

    $('#varsMenu').on('click', togglePBXControls);
    enableVarInput(false);

});
</script>
{{end}}
