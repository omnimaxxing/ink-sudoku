import QtQuick
import "inksudoku.js" as InkSudoku
import "inkkakuro.js" as InkKakuro
import "replay.js" as Replay

// Mac stand-in for AppLoad's backend endpoint: logs what the app sends. For
// applicationID "ink-sudoku" it also answers like the backend would, a moment later, as a
// real backend would: from canned data (inksudoku.js), or with --replay from the real
// backend's recorded messages (replay.js, dev/fixtures/). For "ink-kakuro" it answers from
// canned Kakuro data (inkkakuro.js). Other apps only get the log.
QtObject {
    id: stub
    property string applicationID
    signal messageReceived(int type, string contents)
    function sendMessage(type, contents) {
        console.log("appload send", type, contents);
        const args = Qt.application.arguments;
        const parts = args.indexOf("--parts");
        if (parts >= 0) {
            // A real backend's wire messages for hello ([[type, body], …], see
            // backend/cmd/sudoku/restore_test.go); needs QML_XHR_ALLOW_FILE_READ=1.
            if (type === 100 && !partsSent) {
                partsSent = true;
                const xhr = new XMLHttpRequest();
                xhr.onreadystatechange = () => {
                    if (xhr.readyState === XMLHttpRequest.DONE) {
                        queue = queue.concat(JSON.parse(xhr.responseText));
                        replies.start();
                    }
                };
                xhr.open("GET", "file://" + args[parts + 1]);
                xhr.send();
            }
            return;
        }
        let out;
        if (applicationID === "ink-sudoku")
            out = args.indexOf("--replay") >= 0 ? Replay.reply(type, contents) : InkSudoku.reply(type, contents, args);
        else if (applicationID === "ink-kakuro")
            out = InkKakuro.reply(type, contents, args);
        else
            return;
        queue = queue.concat(out);
        replies.start();
    }
    function terminate() { }

    property bool partsSent: false
    property var queue: []
    property Timer replies: Timer {
        interval: 20
        onTriggered: {
            const q = stub.queue;
            stub.queue = [];
            for (const m of q)
                stub.messageReceived(m[0], m[1]);
        }
    }
}
