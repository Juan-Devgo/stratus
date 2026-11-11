import { render, screen } from "@testing-library/react";

import { NoticeAlert } from "./NoticeAlert";

describe("NoticeAlert", () => {
  it("AC-37 shows the notice under the title Aviso", () => {
    render(<NoticeAlert notice="web-a no tiene registro DNS; agréguelo desde DNS" />);

    expect(screen.getByText("Aviso")).toBeInTheDocument();
    expect(screen.getByText("web-a no tiene registro DNS; agréguelo desde DNS")).toBeInTheDocument();
  });

  it("AC-37 renders nothing without a notice", () => {
    const { container } = render(<NoticeAlert notice="" />);

    expect(container).toBeEmptyDOMElement();
  });
});
