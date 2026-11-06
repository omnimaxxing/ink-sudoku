// Replay mode of the Mac stub (--replay): answers the frontend with the real backend's
// messages recorded in dev/fixtures/session.js (backend/cmd/sudoku/fixtures_test.go).
// Each frontend message takes the next recorded step if the type matches: its replies go
// back with `id` set to the one the frontend sent, and every request field the frontend
// sent differently from the recording is logged ("replay: step N …"). A repeated hello
// gets the latest recorded state; anything else out of turn is logged and not answered.
.pragma library
.import "../../../../fixtures/session.js" as Session

const steps = Session.steps;
let cursor = 0;
let lastState = null;

function same(a, b) {
    return JSON.stringify(a) === JSON.stringify(b);
}

// Logs the fields of the recorded request that the frontend sent differently.
function compare(step, sent) {
    const want = step.send.body;
    for (const k in want) {
        if (k === "id" || k === "seed")
            continue;
        if (!same(want[k], sent[k]))
            console.log("replay: step " + step.step + " (" + step.send.name + "): frontend sent " + k + " = "
                        + JSON.stringify(sent[k]).slice(0, 120) + ", recorded " + JSON.stringify(want[k]).slice(0, 120));
    }
    if ((want.id === undefined) !== (sent.id === undefined))
        console.log("replay: step " + step.step + ": id " + sent.id + ", recorded " + want.id);
}

// Replies to one frontend message: a list of [type, json].
function reply(type, contents) {
    const m = contents ? JSON.parse(contents) : {};
    if (type === 107 || type === 108 || type === 112)   // settings, log, view
        return [];
    const step = steps[cursor];
    if (!step || step.send.type !== type) {
        if (type === 100 && lastState)
            return [[1, JSON.stringify(lastState)]];
        console.log("replay: frontend sent type " + type + ", the session's next is "
                    + (step ? "step " + step.step + " (" + step.send.name + ", " + step.send.type + ")" : "nothing"));
        return [];
    }
    cursor++;
    compare(step, m);
    return step.replies.map(r => {
        let body = r.body;
        if (body.id !== undefined && m.id !== undefined)
            body = Object.assign({}, body, { id: m.id });
        if (r.type === 1)
            lastState = body;
        return [r.type, JSON.stringify(body)];
    });
}

function next() {
    return steps[cursor] || null;
}
