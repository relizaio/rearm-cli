/*
The MIT License (MIT)

Copyright (c) 2020 - 2026 Reliza Incorporated (Reliza (tm), https://reliza.io)

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"),
to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense,
and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

*/

package cmd

import (
	"fmt"

	tea "github.com/oolong-project/tea-client-go"
	"github.com/oolong-project/tea-client-go/teaapi"
)

// The list verbs of the TEA machine mode (task TPF-240): `products` and `components` read
// ONE page of the TEA 1.0.0 product or component list from --api-root and print it as
// TEA_PRODUCT_LIST or TEA_COMPONENT_LIST. One run is one list request: the verbs never walk
// pages, whatever the server says about the next one. A server may answer more rows than
// asked, or its whole list in one page, so the document keeps at most --max-rows rows and
// says when rows were left out.

// validateTeaListFlags checks the flags of products and components, in this order: the
// identifier pair, --sort-order, --page-token, --page-size, --max-rows. The library would
// refuse some of these values itself, but as an invalid argument, which the mode reads as a
// bad answer; so they are refused here first, as usage errors. No message repeats a value
// of --id-value or --page-token.
func validateTeaListFlags(in teaMachineInputs) error {
	typeSet, valueSet := in.set["id-type"], in.set["id-value"]
	if typeSet != valueSet {
		return teaUsage("--id-type and --id-value are given together or not at all")
	}
	if typeSet {
		if !teaapi.IdentifierType(in.idType).Valid() {
			return teaUsage("--id-type must be one of the TEA 1.0.0 identifier types (%s, %s, %s, %s)",
				teaapi.CPE, teaapi.TEI, teaapi.PURL, teaapi.COMPLIANCEDOCUMENT)
		}
		if err := checkTeaPageToken(in.idValue, in.maxStringBytes); err != nil {
			return teaUsage("--id-value %s", err.Error())
		}
	}
	if in.set["sort-order"] {
		if o := tea.SortOrder(in.sortOrder); o != tea.SortAsc && o != tea.SortDesc {
			return teaUsage("--sort-order must be %s or %s", tea.SortAsc, tea.SortDesc)
		}
	}
	if in.pageTokenSet {
		if err := checkTeaPageToken(in.pageToken, in.maxStringBytes); err != nil {
			return teaUsage("--page-token %s", err.Error())
		}
	}
	if err := checkTeaRange("page-size", in.pageSize, teaMinPageSize, teaMaxPageSize); err != nil {
		return err
	}
	return checkTeaRange("max-rows", in.maxRows, teaMinMaxRows, teaMaxMaxRows)
}

// teaListAnswer is one list answer, products or components alike: the kept rows (at most
// --max-rows), how many the server sent, and its next token.
type teaListAnswer struct {
	rows     []teaListRow
	received int
	hasNext  bool
	next     *string
}

// teaListRow is a product or a component as the list keeps it; a TEA 1.0.0 product and
// component have exactly these fields.
type teaListRow struct {
	uuid        string
	name        string
	identifiers []teaapi.Identifier
}

// query makes the one list request of the run and keeps the first --max-rows rows. Fields
// of the query that were not given stay zero, and the library leaves them out of the
// request. sortField is never sent: name is the only field of TEA 1.0.0 and its default.
func (m *teaMachineRun) query(api *tea.API) (teaListAnswer, error) {
	pg := tea.Page{PageSize: m.in.pageSize, SortOrder: tea.SortOrder(m.in.sortOrder)}
	if m.in.pageTokenSet {
		pg.PageToken = m.in.pageToken
	}
	idType := teaapi.IdentifierType(m.in.idType)
	var out teaListAnswer
	keep := func(n int) int { return min(n, m.in.maxRows) }
	if m.in.verb == teaVerbProducts {
		r, err := api.QueryProducts(m.ctx, tea.ProductQuery{Page: pg, IDType: idType, IDValue: m.in.idValue})
		if err != nil {
			return out, err
		}
		out.received, out.hasNext, out.next = len(r.Results), r.HasNext, r.NextPageToken
		for _, p := range r.Results[:keep(len(r.Results))] {
			out.rows = append(out.rows, teaListRow{uuid: p.Uuid.String(), name: p.Name, identifiers: p.Identifiers})
		}
		return out, nil
	}
	r, err := api.QueryComponents(m.ctx, tea.ComponentQuery{Page: pg, IDType: idType, IDValue: m.in.idValue})
	if err != nil {
		return out, err
	}
	out.received, out.hasNext, out.next = len(r.Results), r.HasNext, r.NextPageToken
	for _, c := range r.Results[:keep(len(r.Results))] {
		out.rows = append(out.rows, teaListRow{uuid: c.Uuid.String(), name: c.Name, identifiers: c.Identifiers})
	}
	return out, nil
}

// list: products and components. Only the kept rows are checked against
// --max-string-bytes; rows left out by --max-rows are not part of the document.
func (m *teaMachineRun) list() teaExit {
	api, err := m.api()
	if err != nil {
		return m.fail(err)
	}
	a, err := m.query(api)
	if err != nil {
		return m.fail(err)
	}
	key, op := "products", "querying products"
	if m.in.verb == teaVerbComponents {
		key, op = "components", "querying components"
	}
	sc := m.stringsChecker()
	rows := make([]teaNamedJSON, 0, len(a.rows))
	for i, row := range a.rows {
		p := fmt.Sprintf("%s[%d]", key, i)
		rows = append(rows, teaNamedJSON{UUID: row.uuid, Name: sc.s(p+".name", row.name), Identifiers: sc.identifiers(p+".identifiers", row.identifiers)})
	}
	if sc.err != nil {
		return m.fail(sc.err)
	}
	page := teaItemListPage{RowsReceived: a.received, CutBy: teaListCutNone}
	if a.received > len(rows) {
		page.CutBy = teaListCutMaxRows
	}
	if a.hasNext {
		// the library refuses hasNext without a token, so a.next is set here
		if err := checkTeaPageToken(*a.next, m.in.maxStringBytes); err != nil {
			return m.fail(&tea.UnexpectedResponseError{Operation: op, Status: 200, Reason: "the nextPageToken cannot be passed back: it " + err.Error()})
		}
		page.NextPageToken = a.next
	}
	if m.in.verb == teaVerbComponents {
		return m.emit(teaComponentListDoc{FormatVersion: teaFormatVersion, Kind: teaDocComponentList, Server: m.server(), Components: rows, Page: page}, teaExitOK)
	}
	return m.emit(teaProductListDoc{FormatVersion: teaFormatVersion, Kind: teaDocProductList, Server: m.server(), Products: rows, Page: page}, teaExitOK)
}
