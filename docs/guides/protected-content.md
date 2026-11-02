# Protected content and access requests

Part of a page can be protected: some words, a paragraph, a heading with its section, a picture, a table, a diagram ([ADR-0088](../adr/0088-protected-passages-of-a-body.md), [ADR-0160](../adr/0160-protected-blocks-and-pictures-of-a-page.md)). Only the person who protected it and the people and groups they chose read it. Everyone else who reads the page sees that something is there, not what it is, and can ask to read it ([ADR-0161](../adr/0161-access-requests-are-answered-from-the-task-list.md)).

## What you see when you may not read it

- **Protected words** show as a dark bar inside the sentence: `▒▒▒▒▒▒▒▒`. The bar is always the same length, whatever it hides.
- **Protected blocks** show as one dark card, *Protected content.*, in place of all the blocks of the protection. It does not say whether a picture, a table or a whole section is behind it. Pictures and files of protected blocks are not shown anywhere else either: not in the album, the starmap, exports or copies.
- The same holds for a page embedded in another one: its protected parts are dark there too.
- Below the text, the card *Protected passages* lists each protection of the page by number ("Passage 1", "Protected blocks 2") and says who decides about it.

While a page holds content you may not read, you cannot change its text; you can still change its title, fields, state, labels and comments.

## How to ask for access

1. Click the dark bar or card — or move to it with the Tab key and press Enter. This works on the page itself and inside embedded pages, with the mouse or on a touch screen.
2. A dialog says what is protected (for example *Protected blocks 1 of "Editortest"*) and who decides: the person who protected it, by name, and the administrators of the room.
3. Choose **Ask for access**. The dialog confirms the request and says what happens next.

You need no right to edit the room to ask: reading the page is enough. A request carries no message; if you want to explain why you need it, add a comment to the page.

If you have asked already, clicking again says when you asked and for whom the request waits. A request cannot be withdrawn.

## Where those who decide find the requests

Those who decide are the person who protected the content and the administrators of its room. They are told of each request:

- as a **notification** in the bell;
- as an entry under **Tasks → Access requests**, one per person and protection, across all rooms. Each entry names who asks, links the page, says which protection (numbered as on the page) and when the request was made. The protected content itself is not shown in the list;
- as a count on the start page: on the Tasks tile, in a notice above the tiles, and on the *My tasks* button.

**Grant** lets the person read the protection from then on; they are added to its readers. **Decline** leaves it protected for them. Either way the entry leaves the list at once, because the request no longer waits. Nothing else is created or has to be closed: the list shows the requests as they stand on the pages. Requests can also still be answered in the *Protected passages* card of the page.

Agents read the list with the MCP tool `list_access_requests`; they cannot grant or decline.

## What the one who asked sees afterwards

- While the request waits, the dark bar says *Asked; waiting for an answer*.
- When it is answered, they get a notification: "lets you read a protected passage of …" or "declined your request for a protected passage of …".
- After a grant the content appears in place of the bar the next time the page is shown. After a decline the bar stays and they may ask again.
